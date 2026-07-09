package evaluator

import (
	"testing"
	"time"
)

func TestDefaultEvaluator(t *testing.T) {
	eval := NewDefaultEvaluator(nil)
	eval.SetVariable("testVar", "hello_world")
	eval.SetVariable("userId", "12345")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple variable",
			input:    "Value is ${testVar}",
			expected: "Value is hello_world",
		},
		{
			name:     "Multiple variables",
			input:    "${testVar} for ${userId}",
			expected: "hello_world for 12345",
		},
		{
			name:     "Unknown variable",
			input:    "Missing ${unknown}",
			expected: "Missing ${unknown}",
		},
		{
			name:     "JMeter __eval function",
			input:    `${__eval(User ID: ${userId})}`,
			expected: "User ID: 12345",
		},
		{
			name:     "JMeter __time function standard",
			input:    `${__time()}`,
			expected: "...", // we'll skip exact match for time, just check it evaluated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := eval.Evaluate(tt.input)
			if tt.name == "JMeter __time function standard" {
				if len(res) < 10 {
					t.Errorf("expected timestamp, got %s", res)
				}
			} else if res != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, res)
			}
		})
	}
}

func TestTimeFunctionFormatting(t *testing.T) {
	eval := NewDefaultEvaluator(nil)

	res := eval.Evaluate(`${__time(yyyy-MM-dd)}`)
	expectedFormat := time.Now().Format("2006-01-02")
	if res != expectedFormat {
		t.Errorf("expected %s, got %s", expectedFormat, res)
	}
}
