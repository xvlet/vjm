package domain

// SteppingConfig represents the properties of a SteppingThreadGroup.
type SteppingConfig struct {
	InitialDelay  string
	StepRate      string
	StepDuration  string
	MaxRate       string
	HoldDuration  string
}

// Timer represents JMeter Timer configurations (e.g., ConstantTimer, UniformRandomTimer)
type Timer struct {
	Type  string // "ConstantTimer", "UniformRandomTimer"
	Delay string
	Range string
}

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
	Name           string
	NumThreads     int // TODO: use for per-thread rate control when SteppingThreadGroup is implemented
	RampUp         int // TODO: seconds to ramp up to full load
	Duration       int // TODO: total duration per step
	Samplers       []*Sampler
	SteppingConfig *SteppingConfig
	OpenModelSchedule string // JMeter 5.5+ Open Model Thread Group DSL schedule
	Timers         []*Timer
}

// Sampler represents a JMeter HTTP Sampler
type Sampler struct {
	Name      string
	Request   *RequestTemplate
	Weight    float64
	Extractors []Extractor
}
