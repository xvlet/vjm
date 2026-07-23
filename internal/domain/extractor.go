package domain

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"

	"bytes"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/xmlquery"
	"github.com/jmespath/go-jmespath"
	"github.com/oliveagle/jsonpath"
)

// resolveMatchIndex computes the match index.
func resolveMatchIndex(matchNo, resultsLen int) (int, bool) {
	if resultsLen == 0 {
		return 0, false
	}
	if matchNo < 0 {
		return 0, false // should be handled by ExtractMulti
	}
	if matchNo == 0 {
		return rand.IntN(resultsLen), true
	}
	matchIdx := matchNo - 1
	if matchIdx >= 0 && matchIdx < resultsLen {
		return matchIdx, true
	}
	return 0, false // Out-of-bounds should fail extraction, triggering default value
}

type Extractor interface {
	Extract(body []byte) (string, bool)
	RefName() string
	DefaultValue() (string, bool)
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

func (e *RegexExtractor) RefName() string { return e.ReferenceName }
func (e *RegexExtractor) DefaultValue() (string, bool) {
	if e.DefaultValueStr == "" {
		return "", false
	}
	return e.DefaultValueStr, true
}

func (e *RegexExtractor) Extract(body []byte) (string, bool) {
	if e.compiledRegex == nil {
		return "", false
	}
	matches := e.compiledRegex.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return "", false
	}

	matchIdx, ok := resolveMatchIndex(e.MatchNo, len(matches))
	if !ok {
		return "", false
	}

	match := matches[matchIdx]

	// Handle template replacement, e.g. $1$ or $1$ - $2$
	if e.Template != "" {
		result := e.Template
		for i := 0; i < len(match); i++ {
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

// ExtractMulti handles MatchNo == -1 (all matches)
func (e *RegexExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	// MatchNo == -1 means "all matches"; 0 means random single, >0 means Nth single
	if e.MatchNo != -1 {
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
			for j := 0; j < len(match); j++ {
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

func (e *JSONExtractor) RefName() string { return e.ReferenceName }
func (e *JSONExtractor) DefaultValue() (string, bool) {
	if e.DefaultValueStr == "" {
		return "", false
	}
	return e.DefaultValueStr, true
}

func (e *JSONExtractor) Extract(body []byte) (string, bool) {
	results, ok := EvaluateJSONPathMulti(body, e.JSONPathExpr)
	if !ok || len(results) == 0 {
		return "", false
	}

	matchIdx, ok := resolveMatchIndex(e.MatchNo, len(results))
	if !ok {
		return "", false
	}
	return results[matchIdx], true
}

func (e *JSONExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	// MatchNo == -1 means "all matches"; 0 means random single, >0 means Nth single
	if e.MatchNo != -1 {
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
func (e *HtmlExtractor) DefaultValue() (string, bool) {
	if e.DefaultEmptyValue {
		return "", true
	}
	if e.DefaultValueStr == "" {
		return "", false
	}
	return e.DefaultValueStr, true
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

	matchIdx, ok := resolveMatchIndex(e.MatchNo, len(results))
	if !ok {
		return "", false
	}
	return results[matchIdx], true
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

func (e *JMESPathExtractor) RefName() string { return e.ReferenceName }
func (e *JMESPathExtractor) DefaultValue() (string, bool) {
	if e.DefaultValueStr == "" {
		return "", false
	}
	return e.DefaultValueStr, true
}

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

	matchIdx, ok := resolveMatchIndex(e.MatchNo, len(results))
	if !ok {
		return "", false
	}
	return results[matchIdx], true
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

// BoundaryExtractor implementation
type BoundaryExtractor struct {
	ReferenceName     string
	LBoundary         string
	RBoundary         string
	MatchNo           int
	DefaultValueStr   string
	DefaultEmptyValue bool
}

func (e *BoundaryExtractor) RefName() string { return e.ReferenceName }
func (e *BoundaryExtractor) DefaultValue() (string, bool) {
	if e.DefaultEmptyValue {
		return "", true
	}
	if e.DefaultValueStr == "" {
		return "", false
	}
	return e.DefaultValueStr, true
}

func (e *BoundaryExtractor) extractAll(body []byte) ([]string, bool) {
	if e.LBoundary == "" || e.RBoundary == "" {
		return nil, false
	}

	content := string(body)
	var results []string

	for {
		startIdx := strings.Index(content, e.LBoundary)
		if startIdx == -1 {
			break
		}
		// Move content pointer to just after the LBoundary
		content = content[startIdx+len(e.LBoundary):]

		endIdx := strings.Index(content, e.RBoundary)
		if endIdx == -1 {
			break
		}

		results = append(results, content[:endIdx])
		content = content[endIdx+len(e.RBoundary):]
	}

	if len(results) == 0 {
		return nil, false
	}
	return results, true
}

func (e *BoundaryExtractor) Extract(body []byte) (string, bool) {
	results, ok := e.extractAll(body)
	if !ok || len(results) == 0 {
		return "", false
	}

	matchIdx, ok := resolveMatchIndex(e.MatchNo, len(results))
	if !ok {
		return "", false
	}
	return results[matchIdx], true
}

func (e *BoundaryExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
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

// DebugPostProcessor represents JMeter's Debug PostProcessor.
type DebugPostProcessor struct {
	Name                     string
	DisplayJMeterVariables   bool
	DisplayJMeterProperties  bool
	DisplaySamplerProperties bool
	DisplaySystemProperties  bool
}

// Implement Extractor interface so it can be appended to sampler.Extractors
func (e *DebugPostProcessor) RefName() string              { return "__DEBUG__" }
func (e *DebugPostProcessor) DefaultValue() (string, bool) { return "", false }
func (e *DebugPostProcessor) Extract(body []byte) (string, bool) {
	return "", false
}

// Execute logs the debug information (called directly by the engine)
func (e *DebugPostProcessor) Execute(vars map[string]string, props map[string]string) {
	// In vjm, we print to standard out or use a simple logging mechanism.
	// For load tests this is expensive, but we support it per user request.
	var sb strings.Builder
	sb.WriteString("\n============ Processor: " + e.Name + " ============\n")

	if e.DisplayJMeterVariables {
		sb.WriteString("[JMeterVariables]\n")
		for k, v := range vars {
			sb.WriteString(k + "=" + v + "\n")
		}
	}
	if e.DisplayJMeterProperties {
		sb.WriteString("JMeterProperties:\n")
		for k, v := range props {
			sb.WriteString(k + "=" + v + "\n")
		}
	}
	sb.WriteString("========================================================\n")

	fmt.Print(sb.String())
}

// ResultAction represents JMeter's Result Status Action Handler
type ResultAction struct {
	Action int // 0: Continue, 1: Stop Thread, 2: Stop Test, 3: Stop Test Now, 4: Start Next Loop, 5: Start Next Iteration of Current Loop, 6: Break Current Loop
}

func (r *ResultAction) RefName() string              { return "__RESULT_ACTION__" }
func (r *ResultAction) DefaultValue() (string, bool) { return "", false }
func (r *ResultAction) Extract(body []byte) (string, bool) {
	return "", false
}

// XPathExtractor extracts values using XPath expression from XML/HTML
type XPathExtractor struct {
	ReferenceName string // XPathExtractor.refname
	XPathQuery    string // XPathExtractor.xpathQuery
	DefaultVal    string // XPathExtractor.default
	MatchNumber   int    // XPathExtractor.matchNumber (0: Random, -1: All, >0: Nth)
	Fragment      bool   // XPathExtractor.fragment
	Tolerant      bool   // XPathExtractor.tolerant
	NameSpace     bool   // XPathExtractor.namespace
}

func (e *XPathExtractor) RefName() string {
	return e.ReferenceName
}

func (e *XPathExtractor) DefaultValue() (string, bool) {
	if e.DefaultVal == "" {
		return "", false
	}
	return e.DefaultVal, true
}

func (e *XPathExtractor) Extract(body []byte) (string, bool) {
	res, ok := e.ExtractMulti(body)
	if !ok || len(res) == 0 {
		return "", false
	}
	if val, exists := res[e.ReferenceName]; exists {
		return val, true
	}
	if val, exists := res[e.ReferenceName+"_1"]; exists {
		return val, true
	}
	return "", false
}

func (e *XPathExtractor) ExtractMulti(body []byte) (map[string]string, bool) {
	doc, err := xmlquery.Parse(bytes.NewReader(body))
	if err != nil {
		if e.Tolerant {
			// If tolerant is true, we should ideally use htmlquery, but vjm might not have it loaded.
			// Let's fallback to string manipulation or just fail for now.
			return nil, false
		}
		return nil, false
	}

	nodes, err := xmlquery.QueryAll(doc, e.XPathQuery)
	if err != nil || len(nodes) == 0 {
		return nil, false
	}

	var matches []string
	for _, n := range nodes {
		if e.Fragment {
			matches = append(matches, n.OutputXML(true))
		} else {
			matches = append(matches, n.InnerText())
		}
	}

	if len(matches) == 0 {
		return nil, false
	}

	res := make(map[string]string)

	if e.MatchNumber == 0 {
		// Random
		idx := rand.IntN(len(matches))
		res[e.ReferenceName] = matches[idx]
		return res, true
	} else if e.MatchNumber > 0 {
		// Nth
		if e.MatchNumber <= len(matches) {
			res[e.ReferenceName] = matches[e.MatchNumber-1]
			return res, true
		}
		return nil, false
	}

	// MatchNumber < 0 means "All": populate refName_matchNr and refName_1..N,
	// consistent with JMeter behaviour and other extractors (RegexExtractor, etc.).
	res[e.ReferenceName+"_matchNr"] = strconv.Itoa(len(matches))
	for i, m := range matches {
		res[e.ReferenceName+"_"+strconv.Itoa(i+1)] = m
	}
	return res, true
}
