package domain

import (
	"reflect"
	"testing"
)

func TestEvaluateJSONPathMulti(t *testing.T) {
	jsonBody := []byte(`{
		"store": {
			"book": [
				{
					"category": "reference",
					"author": "Nigel Rees",
					"title": "Sayings of the Century",
					"price": 8.95
				},
				{
					"category": "fiction",
					"author": "Evelyn Waugh",
					"title": "Sword of Honour",
					"price": 12.99
				}
			],
			"bicycle": {
				"color": "red",
				"price": 19.95
			}
		},
		"active": true
	}`)

	tests := []struct {
		name     string
		expr     string
		expected []string
		ok       bool
	}{
		{
			name:     "Simple object extraction",
			expr:     "$.store.bicycle.color",
			expected: []string{"red"},
			ok:       true,
		},
		{
			name:     "Array index positive",
			expr:     "$.store.book[0].author",
			expected: []string{"Nigel Rees"},
			ok:       true,
		},
		{
			name:     "Array index negative",
			expr:     "$.store.book[-1].author",
			expected: []string{"Evelyn Waugh"},
			ok:       true,
		},
		{
			name:     "Wildcard array extraction",
			expr:     "$.store.book[*].category",
			expected: []string{"reference", "fiction"},
			ok:       true,
		},
		{
			name:     "Float conversion",
			expr:     "$.store.bicycle.price",
			expected: []string{"19.95"},
			ok:       true,
		},
		{
			name:     "Boolean conversion",
			expr:     "$.active",
			expected: []string{"true"},
			ok:       true,
		},
		{
			name:     "Not found path",
			expr:     "$.store.car",
			expected: nil,
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, ok := EvaluateJSONPathMulti(jsonBody, tt.expr)
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got %v", tt.ok, ok)
			}
			if !reflect.DeepEqual(res, tt.expected) {
				t.Errorf("expected results %v, got %v", tt.expected, res)
			}
		})
	}
}

func TestRegexExtractorMulti(t *testing.T) {
	body := []byte(`var1=apple&var2=banana&var3=cherry`)

	ext := NewRegexExtractor("fruit", `var\d=([^&]+)`, "$1$", "default_fruit", -1)

	resMap, ok := ext.ExtractMulti(body)
	if !ok {
		t.Fatalf("expected ExtractMulti to succeed")
	}

	if resMap["fruit_matchNr"] != "3" {
		t.Errorf("expected fruit_matchNr=3, got %s", resMap["fruit_matchNr"])
	}
	if resMap["fruit_1"] != "apple" {
		t.Errorf("expected fruit_1=apple, got %s", resMap["fruit_1"])
	}
	if resMap["fruit_2"] != "banana" {
		t.Errorf("expected fruit_2=banana, got %s", resMap["fruit_2"])
	}
	if resMap["fruit_3"] != "cherry" {
		t.Errorf("expected fruit_3=cherry, got %s", resMap["fruit_3"])
	}
}
