package evaluator

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"html"
	"math/rand/v2"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/antchfx/xmlquery"
	"github.com/expr-lang/expr"
)

type evalFunc func(e *DefaultEvaluator, args []string) string

var functionRegistry map[string]evalFunc

func init() {
	functionRegistry = map[string]evalFunc{
		"__time":            evalTime,
		"__jexl3":           evalScript,
		"__groovy":          evalScript,
		"__javaScript":      evalScript,
		"__RandomString":    evalRandomstring,
		"__P":               evalP,
		"__eval":            evalEval,
		"__FileToString":    evalFiletostring,
		"__property":        evalProperty,
		"__V":               evalV,
		"__Random":          evalRandom,
		"__intSum":          evalIntsum,
		"__longSum":         evalLongsum,
		"__UUID":            evalUuid,
		"__urlencode":       evalUrlencode,
		"__urldecode":       evalUrldecode,
		"__toLowerCase":     evalTolowercase,
		"__toUpperCase":     evalTouppercase,
		"__escapeHtml":      evalEscapehtml,
		"__unescapeHtml":    evalUnescapehtml,
		"__machineIP":       evalMachineip,
		"__machineName":     evalMachinename,
		"__md5":             evalMd5,
		"__digest":          evalDigest,
		"__split":           evalSplit,
		"__dateTimeConvert": evalDatetimeconvert,
		"__substring":       evalSubstring,
		"__isPropDefined":   evalIspropdefined,
		"__setProperty":     evalSetproperty,
		"__counter":         evalCounter,
		"__CSVRead":         evalCsvread,
		"__isVarDefined":    evalIsvardefined,
		"__evalVar":         evalEvalvar,
		"__changeCase":      evalChangecase,
		"__char":            evalChar,
		"__XPath":           evalXpath,
	}
}

func evalTime(e *DefaultEvaluator, args []string) string {
	// JMeter spec: no args → Unix epoch milliseconds
	if len(args) == 0 || args[0] == "" {
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	format := mapJavaTimeToGo(args[0])
	return time.Now().Format(format)
}

func evalScript(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	// The first argument is the expression, evaluate inner variables first
	exprStr := e.Evaluate(args[0])
	res, err := expr.Eval(exprStr, nil)
	if err != nil {
		return "false" // Default fallback on error
	}
	return fmt.Sprintf("%v", res)
}

func evalRandomstring(e *DefaultEvaluator, args []string) string {
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
}

func evalP(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	propName := args[0]
	if propVal, ok := e.shared.properties.Load(propName); ok {
		return propVal.(string)
	}
	if len(args) > 1 {
		return args[1] // Default value
	}
	return ""
}

func evalEval(e *DefaultEvaluator, args []string) string {
	if len(args) > 0 {
		return e.Evaluate(args[0])
	}
	return ""
}

func evalFiletostring(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 || args[0] == "" {
		return ""
	}
	fileName := args[0]
	content, err := os.ReadFile(fileName)
	if err == nil {
		return strings.ReplaceAll(string(content), "\r", "")
	}
	return ""
}

func evalProperty(e *DefaultEvaluator, args []string) string {
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

	val := defVal
	if propVal, ok := e.shared.properties.Load(propName); ok {
		val = propVal.(string)
	}

	if varName != "" {
		e.variables[varName] = val
	}
	return val
}

func evalV(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	varName := args[0]
	defVal := ""
	if len(args) > 1 {
		defVal = args[1]
	}
	val, ok := e.variables[varName]
	if ok {
		return val
	}
	if defVal != "" {
		return defVal
	}
	return "${" + varName + "}"
}

func evalRandom(e *DefaultEvaluator, args []string) string {
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
}

func evalIntsum(e *DefaultEvaluator, args []string) string {
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
}

func evalLongsum(e *DefaultEvaluator, args []string) string {
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
}

func evalUuid(e *DefaultEvaluator, args []string) string {
	return generateUUID()
}

func evalUrlencode(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	return url.QueryEscape(args[0])
}

func evalUrldecode(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	decoded, err := url.QueryUnescape(args[0])
	if err != nil {
		return args[0]
	}
	return decoded
}

func evalTolowercase(e *DefaultEvaluator, args []string) string {
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
}

func evalTouppercase(e *DefaultEvaluator, args []string) string {
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
}

func evalEscapehtml(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	return html.EscapeString(args[0])
}

func evalUnescapehtml(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	return html.UnescapeString(args[0])
}

func evalMachineip(e *DefaultEvaluator, args []string) string {
	ip := getLocalIP()
	if len(args) > 0 && args[0] != "" {
		e.mu.Lock()
		e.variables[args[0]] = ip
		e.mu.Unlock()
	}
	return ip
}

func evalMachinename(e *DefaultEvaluator, args []string) string {
	name, _ := os.Hostname()
	if len(args) > 0 && args[0] != "" {
		e.mu.Lock()
		e.variables[args[0]] = name
		e.mu.Unlock()
	}
	return name
}

func evalMd5(e *DefaultEvaluator, args []string) string {
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
}

func evalDigest(e *DefaultEvaluator, args []string) string {
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
}

func evalSplit(e *DefaultEvaluator, args []string) string {
	if len(args) < 2 {
		return ""
	}
	str := args[0]
	varName := args[1]
	delim := ","
	if len(args) > 2 {
		if args[2] != "" {
			delim = args[2]
		}
	}
	tokens := strings.Split(str, delim)
	e.mu.Lock()
	for i, token := range tokens {
		e.variables[fmt.Sprintf("%s_%d", varName, i+1)] = token
	}
	e.variables[varName+"_n"] = strconv.Itoa(len(tokens))
	e.mu.Unlock()
	return str
}

func evalDatetimeconvert(e *DefaultEvaluator, args []string) string {
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
}

func evalSubstring(e *DefaultEvaluator, args []string) string {
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
}

func evalIspropdefined(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return "false"
	}
	_, ok := e.shared.properties.Load(args[0])
	return strconv.FormatBool(ok)
}

func evalSetproperty(e *DefaultEvaluator, args []string) string {
	if len(args) < 2 {
		return ""
	}
	propName := args[0]
	propVal := args[1]
	e.shared.properties.Store(propName, propVal)
	if len(args) > 2 && strings.ToLower(args[2]) == "true" {
		return propVal
	}
	return ""
}

func evalCounter(e *DefaultEvaluator, args []string) string {
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
}

func evalCsvread(e *DefaultEvaluator, args []string) string {
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

	rowIdx, ok := e.csvRows[fileName] // csvRows is session-local; no lock needed
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
}

func evalIsvardefined(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return "false"
	}
	_, ok := e.variables[args[0]]
	return strconv.FormatBool(ok)
}

func evalEvalvar(e *DefaultEvaluator, args []string) string {
	if len(args) == 0 {
		return ""
	}
	if val, ok := e.variables[args[0]]; ok {
		return e.Evaluate(val)
	}
	return ""
}

func evalChangecase(e *DefaultEvaluator, args []string) string {
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
}

func evalChar(e *DefaultEvaluator, args []string) string {
	var sb strings.Builder
	for _, arg := range args {
		if val, err := strconv.ParseInt(strings.TrimSpace(arg), 0, 32); err == nil {
			sb.WriteRune(rune(val))
		}
	}
	return sb.String()
}

func evalXpath(e *DefaultEvaluator, args []string) string {
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
}
