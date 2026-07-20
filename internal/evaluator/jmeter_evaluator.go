package evaluator

import (
	crand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// SharedContext holds thread-safe global states
type SharedContext struct {
	properties  sync.Map
	globalCount int64
	csvCache    sync.Map // maps fileName to *CSVSharedState
	exprCache   sync.Map // maps resolved condition to *vm.Program
}

type CSVSharedState struct {
	lines   [][]string
	nextRow int64
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
	var program *vm.Program
	if cached, ok := e.shared.exprCache.Load(resolved); ok {
		program = cached.(*vm.Program)
	} else {
		var err error
		program, err = expr.Compile(resolved)
		if err != nil {
			return false // On error, treat as false
		}
		e.shared.exprCache.Store(resolved, program)
	}

	res, err := expr.Run(program, nil)
	if err != nil {
		return false
	}

	if b, ok := res.(bool); ok {
		return b
	}
	// If it evaluated to a string "true"/"false"
	if s, ok := res.(string); ok {
		return strings.EqualFold(s, "true")
	}

	return false
}

// DefaultEvaluator processes JMeter variables and functions
type DefaultEvaluator struct {
	shared     *SharedContext
	mu         dummyMutex // e.mu overhead eliminated via inlining
	variables  map[string]string
	localCount int64
	csvRows    map[string]int // maps fileName to current row index for this thread
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
		shared:     e.shared, // Properties are global, share the reference
		variables:  newVars,  // Variables are session-local, create a deep copy
		localCount: 0,        // Counters reset per thread
		csvRows:    make(map[string]int),
	}
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
	if strings.HasPrefix(vName, "A_") {
		countStr := e.variables["__counter"]
		if countStr != "" {
			vName = "A_" + countStr
		}
	}

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

// mapJavaTimeToGo translates common Java SimpleDateFormat strings to Go time layouts
func mapJavaTimeToGo(javaFormat string) string {
	f := strings.ReplaceAll(javaFormat, "yyyy", "2006")
	f = strings.ReplaceAll(f, "MM", "01")
	f = strings.ReplaceAll(f, "dd", "02")
	f = strings.ReplaceAll(f, "HH", "15")
	f = strings.ReplaceAll(f, "mm", "04")
	f = strings.ReplaceAll(f, "ss", "05")
	f = strings.ReplaceAll(f, "SSS", "000")
	return f
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

	for _, char := range s {
		switch char {
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
	args = append(args, strings.TrimSpace(current.String()))
	return args
}
