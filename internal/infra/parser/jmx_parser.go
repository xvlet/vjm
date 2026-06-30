package parser

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"vjm/internal/domain"
)

// DefaultJmxParser parses JMeter JMX XML files to extract the primary HTTP Request configuration.
type DefaultJmxParser struct{}

func NewDefaultJmxParser() *DefaultJmxParser {
	return &DefaultJmxParser{}
}

func (p *DefaultJmxParser) Parse(filePath string) (*domain.TestPlan, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	plan := &domain.TestPlan{
		UserDefinedVariables: make(map[string]string),
	}
	
	var currentThreadGroup *domain.ThreadGroup
	var currentReq *domain.RequestTemplate

	var currentTag, nameAttr, currentHeaderName, currentArgName string
	var inHeaderManager, postBodyRaw bool
	var domainVal, portVal, pathVal, protocolVal string

	for {
		t, err := decoder.Token()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("xml parse error: %w", err)
		}
		if t == nil {
			break
		}

		switch se := t.(type) {
		case xml.StartElement:
			currentTag = se.Name.Local
			nameAttr = ""
			testNameAttr := ""
			for _, attr := range se.Attr {
				if attr.Name.Local == "name" {
					nameAttr = attr.Value
				} else if attr.Name.Local == "testname" {
					testNameAttr = attr.Value
				}
			}
			if testNameAttr != "" && (currentTag == "HTTPSamplerProxy" || strings.HasSuffix(currentTag, "ThreadGroup")) {
				nameAttr = testNameAttr
			}

			if strings.HasSuffix(currentTag, "ThreadGroup") {
				currentThreadGroup = &domain.ThreadGroup{
					Name: nameAttr,
				}
				plan.ThreadGroups = append(plan.ThreadGroups, currentThreadGroup)
			} else if currentTag == "HTTPSamplerProxy" {
				currentReq = &domain.RequestTemplate{
					Headers: make(map[string]string),
				}
				if currentThreadGroup != nil {
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						Name:    nameAttr,
						Request: currentReq,
					})
				}
				// Reset sampler specific variables
				domainVal, portVal, pathVal, protocolVal = "", "", "", ""
				currentHeaderName = ""
				postBodyRaw = false
			} else if currentTag == "HeaderManager" {
				inHeaderManager = true
			}
		case xml.EndElement:
			if se.Name.Local == "HeaderManager" {
				inHeaderManager = false
			} else if se.Name.Local == "HTTPSamplerProxy" && currentReq != nil {
				if protocolVal == "" {
					protocolVal = "http"
				}
				urlStr := protocolVal + "://" + domainVal
				if portVal != "" {
					urlStr += ":" + portVal
				}
				urlStr += pathVal
				currentReq.URL = urlStr
				currentReq = nil
			} else if strings.HasSuffix(se.Name.Local, "ThreadGroup") {
				currentThreadGroup = nil
			}
		case xml.CharData:
			val := strings.TrimSpace(string(se))
			if val == "" {
				continue
			}

			if currentTag == "boolProp" {
				if nameAttr == "HTTPSampler.postBodyRaw" && val == "true" {
					postBodyRaw = true
				}
			} else if currentTag == "stringProp" {
				switch nameAttr {
				case "HTTPSampler.domain":
					domainVal = val
				case "HTTPSampler.port":
					portVal = val
				case "HTTPSampler.path":
					pathVal = val
				case "HTTPSampler.protocol":
					protocolVal = val
				case "HTTPSampler.method":
					if currentReq != nil {
						currentReq.Method = val
					}
				case "Argument.name":
					currentArgName = val
				case "Argument.value":
					if currentReq == nil && currentArgName != "" {
						// Global User Defined Variable
						plan.UserDefinedVariables[currentArgName] = val
					} else if currentReq != nil {
						if postBodyRaw && currentReq.BodyTemplate == "" {
							currentReq.BodyTemplate = val
						}
					}
					currentArgName = "" // Reset after value
				case "Header.name":
					if inHeaderManager {
						currentHeaderName = val
					}
				case "Header.value":
					if inHeaderManager && currentHeaderName != "" && currentReq != nil {
						currentReq.Headers[currentHeaderName] = val
						currentHeaderName = ""
					}
				}
			}
		}
	}

	return plan, nil
}
