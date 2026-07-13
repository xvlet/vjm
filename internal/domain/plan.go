package domain

// SteppingConfig represents the properties of a SteppingThreadGroup.
type SteppingConfig struct {
	InitialDelay string
	StepRate     string
	StepDuration string
	MaxRate      string
	HoldDuration string
}

// ThroughputTimer represents a JMeter Throughput Timer (Constant or Precise)
type ThroughputTimer struct {
	Type       string // "ConstantThroughputTimer" or "PreciseThroughputTimer"
	Throughput string // Target throughput per minute
}

// Timer represents JMeter Timer configurations
type Timer struct {
	Type  string // "ConstantTimer", "UniformRandomTimer", "GaussianRandomTimer", "PoissonRandomTimer", "SyncTimer"
	Delay string
	Range string

	// SyncTimer specific
	GroupSize   string
	TimeoutInMs string
}

// CSVDataSet represents a CSV Data Set Config element.
type CSVDataSet struct {
	Filename        string
	FileEncoding    string
	VariableNames   string
	IgnoreFirstLine bool
	Delimiter       string
	QuotedData      bool
	Recycle         bool
	StopThread      bool
	ShareMode       string
}

// ResultCollector represents a JMeter Listener that writes results to a file
type ResultCollector struct {
	Name         string
	Filename     string
	ErrorLogging bool // true if "ResultCollector.error_logging" is true
}

// BackendListener represents a JMeter Backend Listener
type BackendListener struct {
	Name      string
	Classname string
	Arguments map[string]string
}

// Cookie represents a user-defined cookie in the CookieManager.
type Cookie struct {
	Name   string
	Value  string
	Domain string
	Path   string
	Secure bool
}

// CookieManager represents the HTTP Cookie Manager config element.
type CookieManager struct {
	ClearEachIteration bool
	ControlledByThread bool
	Cookies            []Cookie
}

// CacheManager represents the HTTP Cache Manager config element.
type CacheManager struct {
	ClearEachIteration bool
	UseExpires         bool
	MaxSize            int
}

// DNSCacheManager represents the DNS Cache Manager config element.
type DNSCacheManager struct {
	ClearEachIteration bool
	IsCustomResolver   bool
	Servers            []string
	Hosts              map[string]string // hostname -> IP address
}

// Authorization represents a single auth credential.
type Authorization struct {
	URL       string
	Username  string
	Password  string
	Mechanism string
}

// AuthManager represents the HTTP Authorization Manager config element.
type AuthManager struct {
	ClearEachIteration bool
	AuthList           []Authorization
}

// Counter represents the Counter config element.
type Counter struct {
	Start   string
	End     string
	Incr    string
	Name    string
	Format  string
	PerUser bool
}

// RandomVariable represents the Random Variable config element.
type RandomVariable struct {
	Name         string
	MinimumValue string
	MaximumValue string
	Format       string
	PerThread    bool
	RandomSeed   string
}

// TestPlan represents the top-level JMeter test plan
type TestPlan struct {
	Name                 string
	UserDefinedVariables map[string]string // UDV or User Parameters at Test Plan level
	CSVDataSets          []*CSVDataSet
	ResultCollectors     []*ResultCollector
	BackendListeners     []*BackendListener
	CookieManager        *CookieManager
	CacheManager         *CacheManager
	DNSCacheManager      *DNSCacheManager
	AuthManager          *AuthManager
	Counters             []*Counter
	RandomVariables      []*RandomVariable
	ThroughputTimers     []*ThroughputTimer
	ThreadGroups         []*ThreadGroup
}

// ConcurrencyConfig represents the properties of a bzm - Concurrency Thread Group.
type ConcurrencyConfig struct {
	TargetLevel string
	RampUp      string
	Steps       string
	Hold        string
	Unit        string
}

// UltimateScheduleRecord represents one row in the Ultimate Thread Group schedule.
type UltimateScheduleRecord struct {
	StartThreads string
	InitialDelay string
	StartupTime  string
	HoldLoadFor  string
	ShutdownTime string
}

// UltimateConfig represents the properties of an Ultimate Thread Group.
type UltimateConfig struct {
	Records []UltimateScheduleRecord
}

// ArrivalsConfig represents the properties of a bzm - Arrivals Thread Group.
type ArrivalsConfig struct {
	TargetLevel      string
	RampUp           string
	Steps            string
	Hold             string
	Unit             string
	ConcurrencyLimit string
}

// FreeFormScheduleItem represents one row in the Free-Form Arrivals Thread Group schedule.
type FreeFormScheduleItem struct {
	Start    string
	End      string
	Duration string
}

// FreeFormArrivalsConfig represents the properties of a bzm - Free-Form Arrivals Thread Group.
type FreeFormArrivalsConfig struct {
	Schedule         []FreeFormScheduleItem
	Unit             string
	ConcurrencyLimit string
}

// ThreadGroup represents a JMeter Thread Group.
// NumThreads, RampUp, and Duration are parsed from the JMX but not yet used by the runner.
// These will be leveraged in a future enhancement to support stepped load (e.g. SteppingThreadGroup)
// by running Vegeta in multiple stages and merging the results.
type ThreadGroup struct {
	Name                   string
	ActionType             string // "setup", "main", "teardown"
	NumThreads             int    // TODO: use for per-thread rate control when SteppingThreadGroup is implemented
	RampUp                 int    // TODO: seconds to ramp up to full load
	Duration               int    // TODO: total duration per step
	Samplers               []*Sampler
	SteppingConfig         *SteppingConfig
	ConcurrencyConfig      *ConcurrencyConfig
	UltimateConfig         *UltimateConfig
	ArrivalsConfig         *ArrivalsConfig
	FreeFormArrivalsConfig *FreeFormArrivalsConfig
	OpenModelSchedule      string // JMeter 5.5+ Open Model Thread Group DSL schedule
	Timers                 []*Timer
	CSVDataSets            []*CSVDataSet
	ResultCollectors       []*ResultCollector
	BackendListeners       []*BackendListener
	CookieManager          *CookieManager
	CacheManager           *CacheManager
	DNSCacheManager        *DNSCacheManager
	AuthManager            *AuthManager
	Counters               []*Counter
	RandomVariables        []*RandomVariable
	ThroughputTimers       []*ThroughputTimer
	Assertions             []Assertion
}

// Sampler represents a JMeter HTTP Sampler (or Control Flow Marker)
type Sampler struct {
	Name              string
	Request           *RequestTemplate
	Weight            float64
	IfCondition       string // JEXL3 or Groovy boolean condition string
	TransactionName   string // Name of the Transaction Controller
	TransactionParent bool   // Generate parent sample
	Extractors        []Extractor
	Assertions        []Assertion

	// Loop/While/Critical/ForEach Controller specific fields (Control Flow Markers)
	IsControlFlow          bool   // True if this is a marker (not a real request)
	ControlType            string // "LoopStart", "LoopEnd", "WhileStart", "WhileEnd", "CriticalStart", "CriticalEnd", "ForEachStart", "ForEachEnd"
	LoopId                 int    // Unique ID linking LoopStart and LoopEnd (also used for While, ForEach)
	LoopCountExpr          string // Number of loops or "-1" for forever
	LoopContinue           bool   // If continue_forever is true
	LoopJumpIndex          int    // The index to jump back to (for LoopEnd/WhileEnd/ForEachEnd) or skip to (for WhileStart/ForEachStart)
	BlockEndIndex          int    // The index of the corresponding End node
	WhileCondition         string // Condition for WhileController
	CriticalLockName       string // Lock name for CriticalSectionController
	ForEachInputVal        string // ForEach Controller input variable
	ForEachReturnVal       string // ForEach Controller return variable
	ForEachUseSeparator    bool   // ForEach Controller whether to use "_" separator
	ForEachStartIndex      string // ForEach Controller start index (exclusive)
	ForEachEndIndex        string // ForEach Controller end index (inclusive)
	InterleaveChildStarts  []int
	InterleaveChildEnds    []int
	RandomChildStarts      []int
	RandomChildEnds        []int
	RandomOrderChildStarts []int
	RandomOrderChildEnds   []int
	RuntimeSecondsExpr     string // Runtime Controller runtime in seconds
	SwitchValueExpr        string // Switch Controller value
	SwitchChildStarts      []int
	SwitchChildEnds        []int
	SwitchChildNames       []string
	ModuleTargetNodePath   []string // Module Controller target path
}
