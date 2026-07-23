package evaluator

import (
	crand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"

	"github.com/antchfx/xmlquery"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// SharedContext holds thread-safe global states
type SharedContext struct {
	properties          sync.Map
	globalCount         int64
	csvCache            sync.Map // maps fileName to *CSVSharedState
	exprCache           sync.Map // maps resolved condition to *vm.Program
	xpathCache          sync.Map // maps fileName to *XPathSharedState
	stringFromFileCache sync.Map // maps fileName to *StringFromFileState
}

type StringFromFileState struct {
	lines   []string
	nextRow int64
	once    sync.Once
}

type CSVSharedState struct {
	lines   [][]string
	nextRow int64
	once    sync.Once
}

type XPathSharedState struct {
	doc  *xmlquery.Node
	once sync.Once
}

type dummyMutex struct{}

func (dummyMutex) Lock()    {}
func (dummyMutex) Unlock()  {}
func (dummyMutex) RLock()   {}
func (dummyMutex) RUnlock() {}

// EvaluateLogic evaluates a boolean condition string (JMeter If/While Controller).
func (e *DefaultEvaluator) EvaluateLogic(condition string) bool {
	if condition == "" {
		return true // Default JMeter behavior: empty condition is true (or ignored)
	}

	// 1. Resolve any variables and functions (e.g., __jexl3(...) -> true)
	resolved := strings.TrimSpace(e.Evaluate(condition))

	// 2. If it resolved to "true" or "false" directly
	if strings.EqualFold(resolved, "true") {
		return true
	}
	if strings.EqualFold(resolved, "false") {
		return false
	}

	// 3. Fallback: it might be a raw expression like `"200" == "200"`
	// IMPORTANT: Cache key is the original *condition* (before variable resolution),
	// not the resolved value. Using resolved as the key causes unbounded cache growth
	// because variable values change on every iteration (OOM risk).
	var program *vm.Program
	if cached, ok := e.shared.exprCache.Load(condition); ok {
		program = cached.(*vm.Program)
		// Re-run with the current resolved expression
		res, err := expr.Run(program, nil)
		if err != nil {
			// Program may have been compiled from a different resolved state;
			// fall through to recompile with the current resolved value.
			goto recompile
		}
		if b, ok := res.(bool); ok {
			return b
		}
		if s, ok := res.(string); ok {
			return strings.EqualFold(s, "true")
		}
		return false
	}

recompile:
	{
		var err error
		program, err = expr.Compile(resolved)
		if err != nil {
			return false // On error, treat as false
		}
		// Store compiled program keyed by original condition to prevent unbounded growth
		e.shared.exprCache.Store(condition, program)

		res, err := expr.Run(program, nil)
		if err != nil {
			return false
		}
		if b, ok := res.(bool); ok {
			return b
		}
		if s, ok := res.(string); ok {
			return strings.EqualFold(s, "true")
		}
	}

	return false
}

type DefaultEvaluator struct {
	shared      *SharedContext
	mu          dummyMutex // e.mu overhead eliminated via inlining
	variables   map[string]string
	localCount  int64
	csvRows     map[string]int // maps fileName to current row index for this thread
	threadNum   int
	samplerName string
}

func NewDefaultEvaluator(props map[string]string) *DefaultEvaluator {
	shared := &SharedContext{}
	for k, v := range props {
		shared.properties.Store(k, v)
	}
	return &DefaultEvaluator{
		shared:    shared,
		variables: make(map[string]string),
		csvRows:   make(map[string]int),
	}
}

func (e *DefaultEvaluator) Clone() Evaluator {
	newVars := make(map[string]string)
	e.mu.RLock()
	for k, v := range e.variables {
		newVars[k] = v
	}
	e.mu.RUnlock()
	return &DefaultEvaluator{
		shared:      e.shared, // Properties are global, share the reference
		variables:   newVars,  // Variables are session-local, create a deep copy
		localCount:  0,        // Counters reset per thread
		csvRows:     make(map[string]int),
		threadNum:   e.threadNum,
		samplerName: e.samplerName,
	}
}

func (e *DefaultEvaluator) SetThreadNum(num int) {
	e.threadNum = num
}

func (e *DefaultEvaluator) SetSamplerName(name string) {
	e.samplerName = name
}

// AddProperties merges additional properties into the evaluator
func (e *DefaultEvaluator) AddProperties(props map[string]string) {
	for k, v := range props {
		e.shared.properties.Store(k, v)
	}
}

// AddVariables merges additional variables into the evaluator
func (e *DefaultEvaluator) AddVariables(vars map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range vars {
		e.variables[k] = v
	}
}

// SetVariable sets a single variable for the current session
func (e *DefaultEvaluator) SetVariable(key, value string) {
	// e.variables is session-local; no lock needed for SetVariable after Clone
	e.variables[key] = value
}

// Evaluate performs recursive evaluation of JMeter variables and functions
func (e *DefaultEvaluator) Evaluate(template string) string {
	// [PERF] Fast path: if no variable exists, return immediately to avoid allocations
	if !strings.Contains(template, "${") {
		return template
	}

	// Limit recursion to 10 depths to prevent infinite loops
	var sb strings.Builder
	for pass := 0; pass < 10; pass++ {
		previous := template

		if !strings.Contains(template, "${") {
			break
		}

		sb.Reset()
		// Pre-allocate to reduce slice growth overhead
		sb.Grow(len(template) + 64)

		rest := template
		for {
			start := strings.Index(rest, "${")
			if start == -1 {
				sb.WriteString(rest)
				break
			}

			// Find the matching '}' considering nested '{' and '}'
			depth := 1
			end := -1
			curr := start + 2
			for curr < len(rest) {
				idx := strings.IndexAny(rest[curr:], "{}")
				if idx == -1 {
					break
				}
				curr += idx
				if rest[curr] == '{' {
					depth++
				} else {
					depth--
					if depth == 0 {
						end = curr
						break
					}
				}
				curr++
			}

			if end == -1 {
				sb.WriteString(rest)
				break
			}

			inner := rest[start+2 : end]
			replacement := e.evaluateInner(inner)

			sb.WriteString(rest[:start])
			sb.WriteString(replacement)

			rest = rest[end+1:]
		}

		template = sb.String()
		if template == previous {
			break
		}
	}
	return template
}

func (e *DefaultEvaluator) evaluateInner(inner string) string {
	// It's a JMeter Function
	if strings.HasPrefix(inner, "__") {
		return e.evaluateFunction(inner)
	}

	// It's a Variable
	vName := inner

	val, exists := e.variables[vName]
	if exists {
		return val
	}

	// Fallback to property if variable not found
	if propVal, ok := e.shared.properties.Load(inner); ok {
		return propVal.(string)
	}

	// Unresolved, return original
	return "${" + inner + "}"
}

func (e *DefaultEvaluator) evaluateFunction(funcStr string) string {
	idx := strings.Index(funcStr, "(")
	if idx == -1 {
		return "${" + funcStr + "}"
	}

	funcName := funcStr[:idx]
	argsStr := strings.TrimSuffix(funcStr[idx+1:], ")")

	args := splitArgs(argsStr)

	if fn, ok := functionRegistry[funcName]; ok {
		return fn(e, args)
	}
	return "${" + funcStr + "}"
}

// mapJavaTimeToGo translates common Java SimpleDateFormat strings to Go time layouts.
// Processes longest tokens first to avoid partial replacement collisions.
func mapJavaTimeToGo(javaFormat string) string {
	var result strings.Builder
	i := 0
	for i < len(javaFormat) {
		// 4-char tokens
		if i+4 <= len(javaFormat) {
			four := javaFormat[i : i+4]
			if four == "yyyy" {
				result.WriteString("2006")
				i += 4
				continue
			}
			if four == "EEEE" {
				result.WriteString("Monday")
				i += 4
				continue
			}
			if four == "MMMM" {
				result.WriteString("January")
				i += 4
				continue
			}
		}
		// 3-char tokens
		if i+3 <= len(javaFormat) {
			three := javaFormat[i : i+3]
			if three == "SSS" {
				result.WriteString("000")
				i += 3
				continue
			}
			if three == "EEE" {
				result.WriteString("Mon")
				i += 3
				continue
			}
			if three == "MMM" {
				result.WriteString("Jan")
				i += 3
				continue
			}
		}
		// 2-char tokens
		if i+2 <= len(javaFormat) {
			two := javaFormat[i : i+2]
			switch two {
			case "yy":
				result.WriteString("06")
				i += 2
				continue
			case "MM":
				result.WriteString("01")
				i += 2
				continue
			case "dd":
				result.WriteString("02")
				i += 2
				continue
			case "HH":
				result.WriteString("15")
				i += 2
				continue
			case "hh":
				result.WriteString("03") // 12-hour clock
				i += 2
				continue
			case "mm":
				result.WriteString("04")
				i += 2
				continue
			case "ss":
				result.WriteString("05")
				i += 2
				continue
			}
		}
		// Single-char tokens
		switch javaFormat[i] {
		case 'M':
			result.WriteString("1") // single-digit month (no leading zero)
		case 'd':
			result.WriteString("2") // single-digit day
		case 'H':
			// Go has no single-digit 24h token, closest is "15" but it forces 2 digits if >= 10.
			// Actually Go does not have a native format token for hour 0-23 without leading zero!
			// We will just use "15".
			result.WriteString("15")
		case 'h':
			result.WriteString("3") // 12h hour
		case 'm':
			result.WriteString("4") // single-digit minute
		case 's':
			result.WriteString("5") // single-digit second
		case 'a':
			result.WriteString("PM") // AM/PM
		case 'z':
			result.WriteString("MST")
		case 'Z':
			result.WriteString("-0700")
		default:
			result.WriteByte(javaFormat[i])
		}
		i++
	}
	return result.String()
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func randomString(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.IntN(len(charset))]
	}
	return string(b)
}

func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	depth := 0
	escaped := false

	for _, char := range s {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}

		switch char {
		case '\\':
			escaped = true
		case '(', '{':
			depth++
			current.WriteRune(char)
		case ')', '}':
			depth--
			current.WriteRune(char)
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}
	// If trailing backslash exists, write it
	if escaped {
		current.WriteRune('\\')
	}
	args = append(args, strings.TrimSpace(current.String()))
	return args
}
