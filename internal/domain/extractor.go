package domain

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"

	"bytes"
	"github.com/PuerkitoBio/goquery"
	"github.com/jmespath/go-jmespath"
	"github.com/oliveagle/jsonpath"
)

// Extractor defines the interface for JMeter PostProcessors
type Extractor interface {
	Extract(body []byte) (string, bool)
	RefName() string
	DefaultValue() string
}

// MultiExtractor defines the interface for Extractors that return multiple values (e.g. MatchNo=-1)
type MultiExtractor interface {
	ExtractMulti(body []byte) (map[string]string, bool)
}

// RegexExtractor implementation
type RegexExtractor struct {
	ReferenceName   string
	Regex           string
	Template        string // e.g. $1$
	DefaultValueStr string
	MatchNo         int
	compiledRegex   *regexp.Regexp
}

func NewRegexExtractor(ref, regex, tmpl, def string, matchNo int) *RegexExtractor {
	re, _ := regexp.Compile(regex)
	return &RegexExtractor{
		ReferenceName:   ref,
		Regex:           regex,
		Template:        tmpl,
		DefaultValueStr: def,
		MatchNo:         matchNo,
		compiledRegex:   re,
	}
}

func (e *RegexExtractor) RefName() string      { return e.ReferenceName }
func (e *RegexExtractor) DefaultValue() string { return e.DefaultValueStr }

func (e *RegexExtractor) Extract(body []byte) (string, bool) {
	if e.compiledRegex == nil {
		return "", false
	}
	matches := e.compiledRegex.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return "", false
	}

	matchIdx := e.MatchNo - 1
	if e.MatchNo == 0 {
		// JMeter: MatchNo=0 → random selection among all matches
		matchIdx = rand.IntN(len(matches))
	} else if e.MatchNo < 0 {
		// JMeter: MatchNo=-1 → match all (populate _1, _2 etc.)
		// Multi-variable population requires session-level handling.
		// For single-value extract, fall back to the first match.
		matchIdx = 0
	}

	if matchIdx < 0 || matchIdx >= len(matches) {
		matchIdx = 0 // fallback
	}

	match := matches[matchIdx]

	// Handle template replacement, e.g. $1$ or $1$ - $2$
	if e.Template != "" {
		result := e.Template
		for i := 1; i < len(match); i++ {
			placeholder := "$" + strconv.Itoa(i) + "$"
			result = strings.ReplaceAll(result, placeholder, string(match[i]))
		}
		return result, true
	}

	if len(match) > 1 {
		return string(match[1]), true
	}
	return string(match[0]), true
}

// ExtractMulti handles MatchNo < 0 (all matches)
func (e *RegexExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	if e.MatchNo >= 0 {
		return nil, false
	}
	if e.compiledRegex == nil {
		return nil, false
	}
	matches := e.compiledRegex.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, false
	}

	result := make(map[string]string)
	result[e.ReferenceName+"_matchNr"] = strconv.Itoa(len(matches))

	for i, match := range matches {
		idx := strconv.Itoa(i + 1)
		val := string(match[0])

		if e.Template != "" {
			val = e.Template
			for j := 1; j < len(match); j++ {
				placeholder := "$" + strconv.Itoa(j) + "$"
				val = strings.ReplaceAll(val, placeholder, string(match[j]))
			}
		} else if len(match) > 1 {
			val = string(match[1])
		}

		result[e.ReferenceName+"_"+idx] = val
	}
	return result, true
}

// JSONExtractor implementation
type JSONExtractor struct {
	ReferenceName   string
	JSONPathExpr    string
	DefaultValueStr string
	MatchNo         int
}

func (e *JSONExtractor) RefName() string      { return e.ReferenceName }
func (e *JSONExtractor) DefaultValue() string { return e.DefaultValueStr }

func (e *JSONExtractor) Extract(body []byte) (string, bool) {
	results, ok := EvaluateJSONPathMulti(body, e.JSONPathExpr)
	if !ok || len(results) == 0 {
		return "", false
	}

	matchIdx := e.MatchNo - 1
	if e.MatchNo == 0 {
		matchIdx = rand.IntN(len(results))
	} else if e.MatchNo < 0 {
		matchIdx = 0
	}

	if matchIdx < 0 || matchIdx >= len(results) {
		matchIdx = 0
	}
	return results[matchIdx], true
}

func (e *JSONExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	if e.MatchNo >= 0 {
		return nil, false
	}
	results, ok := EvaluateJSONPathMulti(body, e.JSONPathExpr)
	if !ok || len(results) == 0 {
		return nil, false
	}

	resMap := make(map[string]string)
	resMap[e.ReferenceName+"_matchNr"] = strconv.Itoa(len(results))
	for i, val := range results {
		resMap[e.ReferenceName+"_"+strconv.Itoa(i+1)] = val
	}
	return resMap, true
}

// EvaluateJSONPath returns the first match for testing/legacy purposes.
func EvaluateJSONPath(body []byte, jsonPath string) (string, bool) {
	results, ok := EvaluateJSONPathMulti(body, jsonPath)
	if ok && len(results) > 0 {
		return results[0], true
	}
	return "", false
}

// EvaluateJSONPathMulti evaluates a JSONPath expression using github.com/oliveagle/jsonpath.
func EvaluateJSONPathMulti(body []byte, jsonPathExpr string) ([]string, bool) {
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, false
	}

	// Use oliveagle/jsonpath for full JSONPath evaluation
	res, err := jsonpath.JsonPathLookup(data, jsonPathExpr)
	if err != nil {
		return nil, false
	}

	var strResults []string

	// Check if the result is a slice of items
	if sliceRes, ok := res.([]interface{}); ok {
		for _, item := range sliceRes {
			strResults = append(strResults, stringifyJSON(item))
		}
	} else {
		// Single result
		strResults = append(strResults, stringifyJSON(res))
	}

	if len(strResults) == 0 {
		return nil, false
	}

	return strResults, true
}

func stringifyJSON(current interface{}) string {
	if current != nil {
		switch v := current.(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
		// For maps or slices, we might want to convert them back to JSON strings
		if b, err := json.Marshal(current); err == nil {
			return string(b)
		}
	}
	return ""
}

// HtmlExtractor implementation
type HtmlExtractor struct {
	ReferenceName     string
	Expr              string // CSS Selector
	Attribute         string // Empty means text()
	MatchNo           int
	DefaultValueStr   string
	DefaultEmptyValue bool
}

func (e *HtmlExtractor) RefName() string { return e.ReferenceName }
func (e *HtmlExtractor) DefaultValue() string {
	if e.DefaultEmptyValue {
		return ""
	}
	return e.DefaultValueStr
}

func (e *HtmlExtractor) extractAll(body []byte) ([]string, bool) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, false
	}

	var results []string
	doc.Find(e.Expr).Each(func(i int, s *goquery.Selection) {
		var val string
		if e.Attribute != "" {
			if attr, exists := s.Attr(e.Attribute); exists {
				val = attr
			}
		} else {
			val = s.Text()
		}
		results = append(results, val)
	})

	if len(results) == 0 {
		return nil, false
	}
	return results, true
}

func (e *HtmlExtractor) Extract(body []byte) (string, bool) {
	results, ok := e.extractAll(body)
	if !ok || len(results) == 0 {
		return "", false
	}

	matchIdx := e.MatchNo - 1
	switch e.MatchNo {
	case 0:
		matchIdx = rand.IntN(len(results))
	case -1:
		return "", false // Should be handled by ExtractMulti
	}

	if matchIdx >= 0 && matchIdx < len(results) {
		return results[matchIdx], true
	}
	return "", false
}

func (e *HtmlExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	if e.MatchNo != -1 {
		return nil, false
	}

	results, ok := e.extractAll(body)
	if !ok || len(results) == 0 {
		return nil, false
	}

	multi := make(map[string]string)
	multi[e.ReferenceName+"_matchNr"] = strconv.Itoa(len(results))
	for i, v := range results {
		multi[e.ReferenceName+"_"+strconv.Itoa(i+1)] = v
	}

	return multi, true
}

// JMESPathExtractor implementation
type JMESPathExtractor struct {
	ReferenceName   string
	JmesPathExpr    string
	MatchNo         int
	DefaultValueStr string
}

func (e *JMESPathExtractor) RefName() string      { return e.ReferenceName }
func (e *JMESPathExtractor) DefaultValue() string { return e.DefaultValueStr }

func (e *JMESPathExtractor) extractAll(body []byte) ([]string, bool) {
	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		return nil, false
	}

	result, err := jmespath.Search(e.JmesPathExpr, jsonData)
	if err != nil || result == nil {
		return nil, false
	}

	var strResults []string
	if sliceRes, ok := result.([]interface{}); ok {
		for _, item := range sliceRes {
			strResults = append(strResults, stringifyJSON(item))
		}
	} else {
		strResults = append(strResults, stringifyJSON(result))
	}

	if len(strResults) == 0 {
		return nil, false
	}
	return strResults, true
}

func (e *JMESPathExtractor) Extract(body []byte) (string, bool) {
	results, ok := e.extractAll(body)
	if !ok || len(results) == 0 {
		return "", false
	}

	matchIdx := e.MatchNo - 1
	switch e.MatchNo {
	case 0:
		matchIdx = rand.IntN(len(results))
	case -1:
		return "", false // Should be handled by ExtractMulti
	}

	if matchIdx >= 0 && matchIdx < len(results) {
		return results[matchIdx], true
	}
	return "", false
}

func (e *JMESPathExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	if e.MatchNo != -1 {
		return nil, false
	}

	results, ok := e.extractAll(body)
	if !ok || len(results) == 0 {
		return nil, false
	}

	multi := make(map[string]string)
	multi[e.ReferenceName+"_matchNr"] = strconv.Itoa(len(results))
	for i, v := range results {
		multi[e.ReferenceName+"_"+strconv.Itoa(i+1)] = v
	}

	return multi, true
}
