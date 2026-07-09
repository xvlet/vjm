package domain

// TestConfig represents the configuration passed from CLI or parsed environment
type TestConfig struct {
	JmxFilePath   string
	Properties    map[string]string
	Rate          int
	Duration      string
	Workers       int
	ResultBinPath string
	ResultJtlPath string
	ReportDirPath string
	ForceCLI      bool
}

// RequestTemplate represents the extracted HTTP request information from the JMX file
type RequestTemplate struct {
	Method       string
	URL          string
	Headers      map[string]string
	BodyTemplate string
}
