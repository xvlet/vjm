package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Extractor defines the interface for JMeter PostProcessors
type Extractor interface {
	Extract(body []byte) (string, bool)
	RefName() string
	DefaultValue() string
}

// RegexExtractor implementation
type RegexExtractor struct {
	ReferenceName string
	Regex         string
	Template      string // e.g. $1$
	DefaultValueStr string
	compiledRegex *regexp.Regexp
}

func NewRegexExtractor(ref, regex, tmpl, def string) *RegexExtractor {
	re, _ := regexp.Compile(regex)
	return &RegexExtractor{
		ReferenceName: ref,
		Regex:         regex,
		Template:      tmpl,
		DefaultValueStr: def,
		compiledRegex: re,
	}
}

func (e *RegexExtractor) RefName() string { return e.ReferenceName }
func (e *RegexExtractor) DefaultValue() string { return e.DefaultValueStr }

func (e *RegexExtractor) Extract(body []byte) (string, bool) {
	if e.compiledRegex == nil {
		return "", false
	}
	matches := e.compiledRegex.FindSubmatch(body)
	if len(matches) > 1 {
		// Simplified: just return the first group ($1$)
		return string(matches[1]), true
	}
	return "", false
}

// JSONExtractor implementation
type JSONExtractor struct {
	ReferenceName   string
	JSONPathExpr    string
	DefaultValueStr string
}

func (e *JSONExtractor) RefName() string { return e.ReferenceName }
func (e *JSONExtractor) DefaultValue() string { return e.DefaultValueStr }

func (e *JSONExtractor) Extract(body []byte) (string, bool) {
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", false
	}

	// Basic jsonpath evaluation, e.g., $.token
	path := strings.TrimPrefix(e.JSONPathExpr, "$.")
	parts := strings.Split(path, ".")

	current := data
	for _, p := range parts {
		if p == "" {
			continue
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return "", false
		}
		current, ok = m[p]
		if !ok {
			return "", false
		}
	}

	if str, ok := current.(string); ok {
		return str, true
	}
	return "", false
}
