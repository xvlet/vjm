package evaluator

import (
	"crypto/md5"
	crand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"html"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/antchfx/xmlquery"
)

// SharedContext holds thread-safe global states
type SharedContext struct {
	mu          sync.RWMutex
	properties  map[string]string
	globalCount int64
	csvCache    sync.Map // maps fileName to *CSVSharedState
}

type CSVSharedState struct {
	lines   [][]string
	nextRow int64
}

// DefaultEvaluator processes JMeter variables and functions
type DefaultEvaluator struct {
	shared     *SharedContext
	mu         sync.RWMutex
	variables  map[string]string
	localCount int64
	csvRows    map[string]int // maps fileName to current row index for this thread
}

func NewDefaultEvaluator(props map[string]string) *DefaultEvaluator {
	if props == nil {
		props = make(map[string]string)
	}
	return &DefaultEvaluator{
		shared: &SharedContext{
			properties: props,
		},
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
	e.shared.mu.Lock()
	defer e.shared.mu.Unlock()
	for k, v := range props {
		e.shared.properties[k] = v
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
	e.mu.Lock()
	defer e.mu.Unlock()
	e.variables[key] = value
}

// Evaluate performs recursive evaluation of JMeter variables and functions
func (e *DefaultEvaluator) Evaluate(template string) string {
	// Limit recursion to 10 depths to prevent infinite loops
	for pass := 0; pass < 10; pass++ {
		previous := template
		
		var sb strings.Builder
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
			for i := start + 2; i < len(rest); i++ {
				if rest[i] == '{' {
					depth++
				} else if rest[i] == '}' {
					depth--
					if depth == 0 {
						end = i
						break
					}
				}
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
		e.mu.RLock()
		countStr := e.variables["__counter"]
		e.mu.RUnlock()
		if countStr != "" {
			vName = "A_" + countStr
		}
	}

	e.mu.RLock()
	val, exists := e.variables[vName]
	e.mu.RUnlock()
	if exists {
		return val
	}
	
	// Fallback to property if variable not found
	e.shared.mu.RLock()
	val, ok := e.shared.properties[inner]
	e.shared.mu.RUnlock()
	if ok {
		return val
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

	switch funcName {
	case "__time":
		// JMeter spec: no args → Unix epoch milliseconds
		if len(args) == 0 || args[0] == "" {
			return strconv.FormatInt(time.Now().UnixMilli(), 10)
		}
		format := mapJavaTimeToGo(args[0])
		return time.Now().Format(format)

	case "__RandomString":
		length := 10
		if len(args) > 0 && args[0] != "" {
			if l, err := strconv.Atoi(args[0]); err == nil {
				length = l
			}
		}
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		if len(args) > 1 && args[1] != "" {
			chars = args[1]
		}
		return randomString(length, chars)

	case "__P":
		if len(args) == 0 {
			return ""
		}
		propName := args[0]
		e.shared.mu.RLock()
		val, ok := e.shared.properties[propName]
		e.shared.mu.RUnlock()
		if ok {
			return val
		}
		if len(args) > 1 {
			return args[1] // Default value
		}
		return ""

	case "__eval":
		if len(args) > 0 {
			// The content inside __eval is usually already parsed by the recursive regex
			return args[0]
		}
		return ""

	case "__FileToString":
		if len(args) == 0 || args[0] == "" {
			return ""
		}
		fileName := args[0]
		content, err := os.ReadFile(fileName)
		if err == nil {
			return strings.ReplaceAll(string(content), "\r", "")
		}
		return ""

	case "__property":
		if len(args) == 0 {
			return ""
		}
		propName := args[0]
		varName := ""
		if len(args) > 1 {
			varName = args[1]
		}
		defVal := ""
		if len(args) > 2 {
			defVal = args[2]
		}
		e.shared.mu.RLock()
		val, ok := e.shared.properties[propName]
		e.shared.mu.RUnlock()
		if !ok {
			val = defVal
		}
		if varName != "" {
			e.mu.Lock()
			e.variables[varName] = val
			e.mu.Unlock()
		}
		return val

	case "__V":
		if len(args) == 0 {
			return ""
		}
		varName := args[0]
		defVal := ""
		if len(args) > 1 {
			defVal = args[1]
		}
		e.mu.RLock()
		val, ok := e.variables[varName]
		e.mu.RUnlock()
		if ok {
			return val
		}
		if defVal != "" {
			return defVal
		}
		return "${" + varName + "}"

	case "__Random":
		if len(args) < 2 {
			return ""
		}
		minStr, maxStr := args[0], args[1]
		min, err1 := strconv.Atoi(minStr)
		max, err2 := strconv.Atoi(maxStr)
		if err1 != nil || err2 != nil || min > max {
			return ""
		}
		val := min + rand.IntN(max-min+1)
		valStr := strconv.Itoa(val)
		if len(args) > 2 && args[2] != "" {
			e.mu.Lock()
			e.variables[args[2]] = valStr
			e.mu.Unlock()
		}
		return valStr

	case "__intSum":
		if len(args) < 2 {
			return ""
		}
		sum := 0
		for i := 0; i < len(args)-1; i++ {
			if val, err := strconv.Atoi(args[i]); err == nil {
				sum += val
			}
		}
		sumStr := strconv.Itoa(sum)
		lastArg := args[len(args)-1]
		if val, err := strconv.Atoi(lastArg); err == nil {
			sum += val
			sumStr = strconv.Itoa(sum)
		} else if lastArg != "" {
			e.mu.Lock()
			e.variables[lastArg] = sumStr
			e.mu.Unlock()
		}
		return sumStr

	case "__longSum":
		if len(args) < 2 {
			return ""
		}
		var sum int64 = 0
		for i := 0; i < len(args)-1; i++ {
			if val, err := strconv.ParseInt(args[i], 10, 64); err == nil {
				sum += val
			}
		}
		sumStr := strconv.FormatInt(sum, 10)
		lastArg := args[len(args)-1]
		if val, err := strconv.ParseInt(lastArg, 10, 64); err == nil {
			sum += val
			sumStr = strconv.FormatInt(sum, 10)
		} else if lastArg != "" {
			e.mu.Lock()
			e.variables[lastArg] = sumStr
			e.mu.Unlock()
		}
		return sumStr

	case "__UUID":
		return generateUUID()

	case "__urlencode":
		if len(args) == 0 {
			return ""
		}
		return url.QueryEscape(args[0])

	case "__urldecode":
		if len(args) == 0 {
			return ""
		}
		decoded, err := url.QueryUnescape(args[0])
		if err != nil {
			return args[0]
		}
		return decoded

	case "__toLowerCase":
		if len(args) == 0 {
			return ""
		}
		str := strings.ToLower(args[0])
		if len(args) > 1 && args[1] != "" {
			e.mu.Lock()
			e.variables[args[1]] = str
			e.mu.Unlock()
		}
		return str

	case "__toUpperCase":
		if len(args) == 0 {
			return ""
		}
		str := strings.ToUpper(args[0])
		if len(args) > 1 && args[1] != "" {
			e.mu.Lock()
			e.variables[args[1]] = str
			e.mu.Unlock()
		}
		return str

	case "__escapeHtml":
		if len(args) == 0 {
			return ""
		}
		return html.EscapeString(args[0])

	case "__unescapeHtml":
		if len(args) == 0 {
			return ""
		}
		return html.UnescapeString(args[0])

	case "__machineIP":
		ip := getLocalIP()
		if len(args) > 0 && args[0] != "" {
			e.mu.Lock()
			e.variables[args[0]] = ip
			e.mu.Unlock()
		}
		return ip

	case "__machineName":
		name, _ := os.Hostname()
		if len(args) > 0 && args[0] != "" {
			e.mu.Lock()
			e.variables[args[0]] = name
			e.mu.Unlock()
		}
		return name

	case "__md5":
		if len(args) == 0 {
			return ""
		}
		hash := md5.Sum([]byte(args[0]))
		md5str := hex.EncodeToString(hash[:])
		if len(args) > 1 && args[1] != "" {
			e.mu.Lock()
			e.variables[args[1]] = md5str
			e.mu.Unlock()
		}
		return md5str

	case "__digest":
		if len(args) < 2 {
			return ""
		}
		algo := strings.ToUpper(args[0])
		str := args[1]
		salt := ""
		upper := false
		varName := ""
		if len(args) > 2 {
			salt = args[2]
		}
		if len(args) > 3 && strings.ToLower(args[3]) == "true" {
			upper = true
		}
		if len(args) > 4 {
			varName = args[4]
		}

		var hashStr string
		data := []byte(str + salt)
		switch algo {
		case "MD5":
			h := md5.Sum(data)
			hashStr = hex.EncodeToString(h[:])
		case "SHA-1":
			h := sha1.Sum(data)
			hashStr = hex.EncodeToString(h[:])
		case "SHA-256":
			h := sha256.Sum256(data)
			hashStr = hex.EncodeToString(h[:])
		case "SHA-512":
			h := sha512.Sum512(data)
			hashStr = hex.EncodeToString(h[:])
		default:
			return ""
		}

		if upper {
			hashStr = strings.ToUpper(hashStr)
		}
		if varName != "" {
			e.mu.Lock()
			e.variables[varName] = hashStr
			e.mu.Unlock()
		}
		return hashStr

	case "__split":
		if len(args) < 2 {
			return ""
		}
		str := args[0]
		varName := args[1]
		delim := ","
		if len(args) > 2 {
			delim = args[2]
		}
		tokens := strings.Split(str, delim)
		e.mu.Lock()
		for i, token := range tokens {
			e.variables[fmt.Sprintf("%s_%d", varName, i+1)] = token
		}
		e.variables[varName+"_n"] = strconv.Itoa(len(tokens))
		e.mu.Unlock()
		return str

	case "__dateTimeConvert":
		if len(args) < 3 {
			return ""
		}
		dateStr := args[0]
		sourceFmt := args[1]
		targetFmt := args[2]
		varName := ""
		if len(args) > 3 {
			varName = args[3]
		}
		var t time.Time
		var err error
		if sourceFmt == "" {
			// Assume epoch in milliseconds if source format is omitted
			if ms, err2 := strconv.ParseInt(dateStr, 10, 64); err2 == nil {
				t = time.UnixMilli(ms)
			} else {
				return dateStr
			}
		} else {
			t, err = time.Parse(mapJavaTimeToGo(sourceFmt), dateStr)
			if err != nil {
				return dateStr
			}
		}
		res := t.Format(mapJavaTimeToGo(targetFmt))
		if varName != "" {
			e.mu.Lock()
			e.variables[varName] = res
			e.mu.Unlock()
		}
		return res

	case "__substring":
		if len(args) < 3 {
			return ""
		}
		str := args[0]
		begin, err1 := strconv.Atoi(args[1])
		end, err2 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil {
			return str
		}
		runes := []rune(str)
		if begin < 0 {
			begin = 0
		}
		if end > len(runes) {
			end = len(runes)
		}
		if begin > end {
			return str
		}
		res := string(runes[begin:end])
		if len(args) > 3 && args[3] != "" {
			e.mu.Lock()
			e.variables[args[3]] = res
			e.mu.Unlock()
		}
		return res

	case "__isPropDefined":
		if len(args) == 0 {
			return "false"
		}
		e.shared.mu.RLock()
		_, ok := e.shared.properties[args[0]]
		e.shared.mu.RUnlock()
		return strconv.FormatBool(ok)

	case "__setProperty":
		if len(args) < 2 {
			return ""
		}
		propName := args[0]
		propVal := args[1]
		e.shared.mu.Lock()
		e.shared.properties[propName] = propVal
		e.shared.mu.Unlock()
		if len(args) > 2 && strings.ToLower(args[2]) == "true" {
			return propVal
		}
		return ""

	case "__counter":
		if len(args) < 1 {
			return ""
		}
		isGlobal := strings.ToUpper(args[0]) == "TRUE"
		var val int64
		if isGlobal {
			val = atomic.AddInt64(&e.shared.globalCount, 1)
		} else {
			e.mu.Lock()
			e.localCount++
			val = e.localCount
			e.mu.Unlock()
		}
		valStr := strconv.FormatInt(val, 10)
		if len(args) > 1 {
			varName := args[1]
			e.mu.Lock()
			e.variables[varName] = valStr
			e.variables["__counter"] = valStr
			e.mu.Unlock()
		}
		return valStr

	case "__CSVRead":
		if len(args) < 2 {
			return ""
		}
		fileName := args[0]
		param := args[1]

		var csvState *CSVSharedState
		if val, ok := e.shared.csvCache.Load(fileName); ok {
			csvState = val.(*CSVSharedState)
		} else {
			content, err := os.ReadFile(fileName)
			if err != nil {
				return ""
			}
			var lines [][]string
			for _, line := range strings.Split(string(content), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					lines = append(lines, strings.Split(line, ","))
				}
			}
			csvState = &CSVSharedState{
				lines: lines,
			}
			actual, _ := e.shared.csvCache.LoadOrStore(fileName, csvState)
			csvState = actual.(*CSVSharedState)
		}

		if len(csvState.lines) == 0 {
			return ""
		}

		e.mu.RLock()
		rowIdx, ok := e.csvRows[fileName]
		e.mu.RUnlock()
		if !ok {
			rowIdx = int(atomic.AddInt64(&csvState.nextRow, 1) - 1)
			e.mu.Lock()
			e.csvRows[fileName] = rowIdx
			e.mu.Unlock()
		}

		if strings.ToLower(param) == "next" || strings.ToLower(param) == "next()" {
			e.mu.Lock()
			idx := e.csvRows[fileName]
			e.csvRows[fileName] = idx + 1
			e.mu.Unlock()
			return ""
		}

		colIdx, err := strconv.Atoi(param)
		if err != nil || colIdx < 0 {
			return ""
		}

		actualRow := rowIdx % len(csvState.lines)
		row := csvState.lines[actualRow]
		if colIdx < len(row) {
			return row[colIdx]
		}
		return ""

	case "__isVarDefined":
		if len(args) == 0 {
			return "false"
		}
		_, ok := e.variables[args[0]]
		return strconv.FormatBool(ok)

	case "__evalVar":
		if len(args) == 0 {
			return ""
		}
		if val, ok := e.variables[args[0]]; ok {
			return e.Evaluate(val)
		}
		return ""

	case "__changeCase":
		if len(args) < 2 {
			return ""
		}
		str := args[0]
		mode := strings.ToUpper(args[1])
		switch mode {
		case "UPPER":
			str = strings.ToUpper(str)
		case "LOWER":
			str = strings.ToLower(str)
		case "CAPITALIZE":
			if len(str) > 0 {
				runes := []rune(str)
				runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
				str = string(runes)
			}
		}
		if len(args) > 2 && args[2] != "" {
			e.variables[args[2]] = str
		}
		return str

	case "__char":
		var sb strings.Builder
		for _, arg := range args {
			if val, err := strconv.ParseInt(strings.TrimSpace(arg), 0, 32); err == nil {
				sb.WriteRune(rune(val))
			}
		}
		return sb.String()

	case "__XPath":
		if len(args) < 2 {
			return ""
		}
		fileName := args[0]
		xpathExpr := args[1]

		f, err := os.Open(fileName)
		if err != nil {
			return ""
		}
		defer func() { _ = f.Close() }()

		doc, err := xmlquery.Parse(f)
		if err != nil {
			return ""
		}

		node, err := xmlquery.Query(doc, xpathExpr)
		if err != nil || node == nil {
			return ""
		}
		return node.InnerText()

	default:
		return "${" + funcStr + "}"
	}
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
