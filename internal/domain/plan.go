package domain

// TestPlan represents the top-level JMeter test plan
type TestPlan struct {
	Name                 string
	UserDefinedVariables map[string]string // UDV or User Parameters at Test Plan level
	ThreadGroups         []*ThreadGroup
}

// ThreadGroup represents a JMeter Thread Group.
// NumThreads, RampUp, and Duration are parsed from the JMX but not yet used by the runner.
// These will be leveraged in a future enhancement to support stepped load (e.g. SteppingThreadGroup)
// by running Vegeta in multiple stages and merging the results.
type ThreadGroup struct {
	Name       string
	NumThreads int // TODO: use for per-thread rate control when SteppingThreadGroup is implemented
	RampUp     int // TODO: seconds to ramp up to full load
	Duration   int // TODO: total duration per step
	Samplers   []*Sampler
}

// Sampler represents a JMeter HTTP Sampler
type Sampler struct {
	Name    string
	Request *RequestTemplate
}
