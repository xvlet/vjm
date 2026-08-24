package parser

import (
	"github.com/xvlet/vjm/internal/domain"
)

// DefaultJmxParser parses JMeter JMX XML files to extract the primary HTTP Request configuration.
// It delegates directly to JmxParserV2.
type DefaultJmxParser struct{}

func NewDefaultJmxParser() *DefaultJmxParser {
	return &DefaultJmxParser{}
}

func (p *DefaultJmxParser) Parse(filePath string) (*domain.TestPlan, error) {
	return NewJmxParserV2().Parse(filePath)
}
