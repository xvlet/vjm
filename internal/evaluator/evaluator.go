package evaluator

// Evaluator evaluates JMeter functions and variables
type Evaluator interface {
	Evaluate(template string) string
	AddProperties(props map[string]string)
	AddVariables(vars map[string]string)
	SetVariable(key, value string)
	Clone() Evaluator
}
