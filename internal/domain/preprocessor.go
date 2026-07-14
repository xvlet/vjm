package domain

// PreProcessor represents a JMeter PreProcessor
type PreProcessor interface {
	IsPreProcessor() bool
}

// HTMLLinkParser represents the JMeter HTML Link Parser
type HTMLLinkParser struct {
	Name string
}

func (*HTMLLinkParser) IsPreProcessor() bool { return true }
