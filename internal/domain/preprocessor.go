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

// URLRewritingModifier represents the JMeter HTTP URL Re-writing Modifier
type URLRewritingModifier struct {
	Name                        string
	ArgumentName                string
	PathExtension               bool
	PathExtensionNoEq           bool
	PathExtensionNoQuestionMark bool
	ShouldCache                 bool
	Encode                      bool
}

func (*URLRewritingModifier) IsPreProcessor() bool { return true }
