package evaluator

import (
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultEvaluator processes JMeter variables and functions
type DefaultEvaluator struct {
	properties map[string]string
	regex      *regexp.Regexp
}

func NewDefaultEvaluator(props map[string]string) *DefaultEvaluator {
	if props == nil {
		props = make(map[string]string)
	}
	return &DefaultEvaluator{
		properties: props,
		// Matches ${...} non-greedily. Nested ${} is handled via recursive evaluation.
		regex: regexp.MustCompile(`\$\{([^}]+)\}`),
	}
}

// AddProperties merges additional properties into the evaluator
func (e *DefaultEvaluator) AddProperties(props map[string]string) {
	for k, v := range props {
		e.properties[k] = v
	}
}

// Evaluate performs recursive evaluation of JMeter variables and functions
func (e *DefaultEvaluator) Evaluate(template string) string {
	// Limit recursion to 10 depths to prevent infinite loops
	for i := 0; i < 10; i++ {
		matches := e.regex.FindAllStringSubmatch(template, -1)
		if len(matches) == 0 {
			break
		}

		previous := template

		for _, match := range matches {
			fullMatch := match[0] // e.g. ${__time(yyyy)}
			inner := match[1]     // e.g. __time(yyyy)

			replacement := e.evaluateInner(inner)
			template = strings.Replace(template, fullMatch, replacement, 1)
		}

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
	if val, ok := e.properties[inner]; ok {
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
		format := "20060102150405" // fallback
		if len(args) > 0 && args[0] != "" {
			format = mapJavaTimeToGo(args[0])
		}
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
		if val, ok := e.properties[propName]; ok {
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
			return string(content)
		}
		return ""

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
		case '(':
			depth++
			current.WriteRune(char)
		case ')':
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
