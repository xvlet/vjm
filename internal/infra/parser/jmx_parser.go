package parser

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/xvlet/vjm/internal/domain"
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
	defer func() { _ = file.Close() }()

	decoder := xml.NewDecoder(file)
	plan := &domain.TestPlan{
		UserDefinedVariables: make(map[string]string),
	}

	var currentThreadGroup *domain.ThreadGroup
	// samplerStack tracks open HTTPSamplerProxy elements so HeaderManager (in sibling hashTree) can attach to the right request.
	var lastCompletedReq *domain.RequestTemplate
	var currentTimer *domain.Timer

	var currentTag, nameAttr, currentHeaderName, currentArgName string
	var inHeaderManager, postBodyRaw, inConfigTestElement bool
	var currentReq *domain.RequestTemplate
	var domainVal, portVal, pathVal, protocolVal string
	var defDomain, defPort, defPath, defProtocol string

	var inUserParameters bool
	var userParamState string
	var userParamNames []string
	var userParamValues []string

	var hashTreeDepth int
	var pendingWeight float64
	var activeWeight = 1.0
	weightMap := make(map[int]float64)

	var inFloatProperty bool
	var floatPropName string
	var floatPropNameState bool
	var floatPropValueState bool

	var inJSONExtractor, inRegexExtractor bool
	var currentJSONExtractor *domain.JSONExtractor
	var currentRegexExtractor *domain.RegexExtractor

	var inUltimateData bool
	var inUltimateRow bool
	var ultimateRowVals []string

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
			enabledAttr := "true"
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "name":
					nameAttr = attr.Value
				case "testname":
					testNameAttr = attr.Value
				case "enabled":
					enabledAttr = attr.Value
				}
			}
			if testNameAttr != "" && (currentTag == "HTTPSamplerProxy" || strings.HasSuffix(currentTag, "ThreadGroup") || currentTag == "ThroughputController") {
				nameAttr = testNameAttr
			}

			if currentTag == "hashTree" {
				hashTreeDepth++
				if pendingWeight > 0 {
					weightMap[hashTreeDepth] = pendingWeight
					activeWeight = pendingWeight
					pendingWeight = 0
				}
			}

			if strings.HasSuffix(currentTag, "ThreadGroup") {
				currentThreadGroup = &domain.ThreadGroup{
					Name: nameAttr,
				}
				switch currentTag {
				case "kg.apc.jmeter.threads.SteppingThreadGroup":
					currentThreadGroup.SteppingConfig = &domain.SteppingConfig{}
				case "com.blazemeter.jmeter.threads.concurrency.ConcurrencyThreadGroup":
					currentThreadGroup.ConcurrencyConfig = &domain.ConcurrencyConfig{}
				case "kg.apc.jmeter.threads.UltimateThreadGroup":
					currentThreadGroup.UltimateConfig = &domain.UltimateConfig{}
				}
				plan.ThreadGroups = append(plan.ThreadGroups, currentThreadGroup)
				lastCompletedReq = nil
			} else if currentTag == "HTTPSamplerProxy" {
				currentReq = &domain.RequestTemplate{
					Headers: make(map[string]string),
				}
				if currentThreadGroup != nil {
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						Name:    nameAttr,
						Request: currentReq,
						Weight:  activeWeight,
					})
				}
				// Reset sampler-specific URL parts
				domainVal, portVal, pathVal, protocolVal = "", "", "", ""
				currentHeaderName = ""
				postBodyRaw = false
			} else if currentTag == "HeaderManager" {
				inHeaderManager = true
			} else if currentTag == "ConfigTestElement" {
				inConfigTestElement = true
			} else if currentTag == "UserParameters" {
				inUserParameters = true
				userParamNames = []string{}
				userParamValues = []string{}
			} else if currentTag == "collectionProp" {
				switch nameAttr {
				case "UserParameters.names":
					userParamState = "names"
				case "UserParameters.thread_values":
					userParamState = "values"
				case "ultimatethreadgroupdata":
					inUltimateData = true
				default:
					if inUltimateData {
						inUltimateRow = true
						ultimateRowVals = []string{}
					}
				}
			} else if currentTag == "FloatProperty" {
				inFloatProperty = true
				floatPropName = ""
			} else if currentTag == "name" && inFloatProperty {
				floatPropNameState = true
			} else if currentTag == "value" && inFloatProperty {
				floatPropValueState = true
			} else if currentTag == "ConstantTimer" || currentTag == "UniformRandomTimer" {
				if enabledAttr != "false" {
					currentTimer = &domain.Timer{
						Type: currentTag,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.Timers = append(currentThreadGroup.Timers, currentTimer)
					}
				}
			} else if currentTag == "JSONPostProcessor" {
				inJSONExtractor = true
				currentJSONExtractor = &domain.JSONExtractor{}
			} else if currentTag == "RegexExtractor" {
				inRegexExtractor = true
				currentRegexExtractor = &domain.RegexExtractor{}
			}

		case xml.EndElement:
			if se.Name.Local == "hashTree" {
				delete(weightMap, hashTreeDepth)
				hashTreeDepth--
				activeWeight = 1.0
				for d := hashTreeDepth; d >= 0; d-- {
					if w, ok := weightMap[d]; ok {
						activeWeight = w
						break
					}
				}
			} else if se.Name.Local == "HeaderManager" {
				inHeaderManager = false
			} else if se.Name.Local == "ConfigTestElement" {
				inConfigTestElement = false
			} else if se.Name.Local == "FloatProperty" {
				inFloatProperty = false
			} else if se.Name.Local == "name" && inFloatProperty {
				floatPropNameState = false
			} else if se.Name.Local == "value" && inFloatProperty {
				floatPropValueState = false
			} else if se.Name.Local == "JSONPostProcessor" {
				inJSONExtractor = false
				if currentJSONExtractor != nil && currentThreadGroup != nil {
					if len(currentThreadGroup.Samplers) > 0 {
						lastSampler := currentThreadGroup.Samplers[len(currentThreadGroup.Samplers)-1]
						lastSampler.Extractors = append(lastSampler.Extractors, currentJSONExtractor)
					}
				}
				currentJSONExtractor = nil
			} else if se.Name.Local == "RegexExtractor" {
				inRegexExtractor = false
				if currentRegexExtractor != nil && currentThreadGroup != nil {
					if len(currentThreadGroup.Samplers) > 0 {
						lastSampler := currentThreadGroup.Samplers[len(currentThreadGroup.Samplers)-1]
						lastSampler.Extractors = append(lastSampler.Extractors, domain.NewRegexExtractor(
							currentRegexExtractor.ReferenceName,
							currentRegexExtractor.Regex,
							currentRegexExtractor.Template,
							currentRegexExtractor.DefaultValueStr,
						))
					}
				}
				currentRegexExtractor = nil
			} else if se.Name.Local == "ConstantTimer" || se.Name.Local == "UniformRandomTimer" {
				currentTimer = nil
			} else if se.Name.Local == "HTTPSamplerProxy" && currentReq != nil {
				// Build the URL now. Keep currentReq alive so the following
				// HeaderManager (inside sibling hashTree) can still attach headers.
				pcol := protocolVal
				if pcol == "" {
					pcol = defProtocol
				}
				if pcol == "" {
					pcol = "http"
				}

				dom := domainVal
				if dom == "" {
					dom = defDomain
				}

				prt := portVal
				if prt == "" {
					prt = defPort
				}

				pth := pathVal
				if pth == "" {
					pth = defPath
				}

				urlStr := pcol + "://" + dom
				if prt != "" {
					urlStr += ":" + prt
				}
				urlStr += pth
				currentReq.URL = urlStr

				// Move to lastCompletedReq so HeaderManager can still find it,
				// but clear currentReq so subsequent Argument.value parsing doesn't pollute it.
				lastCompletedReq = currentReq
				currentReq = nil
			} else if se.Name.Local == "UserParameters" {
				inUserParameters = false
				for i := 0; i < len(userParamNames) && i < len(userParamValues); i++ {
					plan.UserDefinedVariables[userParamNames[i]] = userParamValues[i]
				}
				userParamState = ""
			} else if se.Name.Local == "collectionProp" {
				if inUltimateRow {
					inUltimateRow = false
					if currentThreadGroup != nil && currentThreadGroup.UltimateConfig != nil && len(ultimateRowVals) >= 5 {
						currentThreadGroup.UltimateConfig.Records = append(currentThreadGroup.UltimateConfig.Records, domain.UltimateScheduleRecord{
							StartThreads: ultimateRowVals[0],
							InitialDelay: ultimateRowVals[1],
							StartupTime:  ultimateRowVals[2],
							HoldLoadFor:  ultimateRowVals[3],
							ShutdownTime: ultimateRowVals[4],
						})
					}
				} else if inUltimateData {
					inUltimateData = false
				}
			}

		case xml.CharData:
			val := strings.TrimSpace(string(se))
			if val == "" {
				continue
			}

			switch currentTag {
			case "name":
				if floatPropNameState {
					floatPropName = val
				}
			case "value":
				if floatPropValueState {
					if floatPropName == "ThroughputController.percentThroughput" {
						if v, err := strconv.ParseFloat(val, 64); err == nil {
							pendingWeight = v
						}
					}
				}
			case "boolProp":
				if nameAttr == "HTTPSampler.postBodyRaw" && val == "true" {
					postBodyRaw = true
				}
			case "stringProp":
				if inUltimateRow {
					ultimateRowVals = append(ultimateRowVals, val)
					continue
				}
				switch nameAttr {
				case "HTTPSampler.domain":
					if inConfigTestElement {
						defDomain = val
					} else {
						domainVal = val
					}
				case "HTTPSampler.port":
					if inConfigTestElement {
						defPort = val
					} else {
						portVal = val
					}
				case "HTTPSampler.path":
					if inConfigTestElement {
						defPath = val
					} else {
						pathVal = val
					}
				case "ConstantTimer.delay":
					if currentTimer != nil {
						currentTimer.Delay = val
					}
				case "RandomTimer.range":
					if currentTimer != nil {
						currentTimer.Range = val
					}
				case "HTTPSampler.protocol":
					if inConfigTestElement {
						defProtocol = val
					} else {
						protocolVal = val
					}
				case "HTTPSampler.method":
					if currentReq != nil {
						currentReq.Method = val
					}
				case "ThreadGroup.num_threads":
					if currentThreadGroup != nil {
						v, _ := strconv.Atoi(val)
						currentThreadGroup.NumThreads = v
						if currentThreadGroup.SteppingConfig != nil {
							currentThreadGroup.SteppingConfig.MaxRate = val
						}
					}
				case "OpenModelThreadGroup.schedule":
					if currentThreadGroup != nil {
						currentThreadGroup.OpenModelSchedule = val
					}
				case "Threads initial delay":
					if currentThreadGroup != nil && currentThreadGroup.SteppingConfig != nil {
						currentThreadGroup.SteppingConfig.InitialDelay = val
					}
				case "Start users count":
					if currentThreadGroup != nil && currentThreadGroup.SteppingConfig != nil {
						currentThreadGroup.SteppingConfig.StepRate = val
					}
				case "Start users period":
					if currentThreadGroup != nil && currentThreadGroup.SteppingConfig != nil {
						currentThreadGroup.SteppingConfig.StepDuration = val
					}
				case "flighttime":
					if currentThreadGroup != nil && currentThreadGroup.SteppingConfig != nil {
						currentThreadGroup.SteppingConfig.HoldDuration = val
					}
				case "TargetLevel":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.TargetLevel = val
					}
				case "RampUp":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.RampUp = val
					}
				case "Steps":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Steps = val
					}
				case "Hold":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Hold = val
					}
				case "Unit":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Unit = val
					}
				case "ThroughputController.maxThroughput":
					if v, err := strconv.ParseFloat(val, 64); err == nil {
						pendingWeight = v
					}
				case "Argument.name":
					currentArgName = val
				case "Argument.value":
					if currentReq == nil && currentArgName != "" {
						// Global User Defined Variable (outside any sampler)
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
					if inHeaderManager && currentHeaderName != "" {
						// Attach to whichever request is active: open or just-completed
						target := currentReq
						if target == nil {
							target = lastCompletedReq
						}
						if target != nil {
							target.Headers[currentHeaderName] = val
						}
						currentHeaderName = ""
					}
				case "JSONPostProcessor.referenceNames":
					if inJSONExtractor && currentJSONExtractor != nil {
						currentJSONExtractor.ReferenceName = val
					}
				case "JSONPostProcessor.jsonPathExprs":
					if inJSONExtractor && currentJSONExtractor != nil {
						currentJSONExtractor.JSONPathExpr = val
					}
				case "JSONPostProcessor.defaultValues":
					if inJSONExtractor && currentJSONExtractor != nil {
						currentJSONExtractor.DefaultValueStr = val
					}
				case "RegexExtractor.refname":
					if inRegexExtractor && currentRegexExtractor != nil {
						currentRegexExtractor.ReferenceName = val
					}
				case "RegexExtractor.regex":
					if inRegexExtractor && currentRegexExtractor != nil {
						currentRegexExtractor.Regex = val
					}
				case "RegexExtractor.template":
					if inRegexExtractor && currentRegexExtractor != nil {
						currentRegexExtractor.Template = val
					}
				case "RegexExtractor.default":
					if inRegexExtractor && currentRegexExtractor != nil {
						currentRegexExtractor.DefaultValueStr = val
					}
				default:
					// Handle UserParameters variables
					if inUserParameters {
						switch userParamState {
						case "names":
							userParamNames = append(userParamNames, val)
						case "values":
							userParamValues = append(userParamValues, val)
						}
					}
			}
			}
		}
	}

	return plan, nil
}
