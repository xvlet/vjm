package parser

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	var pendingIfCondition string
	ifConditionMap := make(map[int]string)

	var pendingTransactionName string
	var pendingTransactionParent bool
	transactionNameMap := make(map[int]string)
	transactionParentMap := make(map[int]bool)

	type LoopContext struct {
		Depth      int
		LoopId     int
		StartIndex int
		IsWhile    bool
		IsCritical bool
		IsForEach  bool
	}
	var loopStack []LoopContext
	var pendingLoopId int
	var pendingLoopCountExpr string
	var pendingLoopContinue bool
	var pendingWhileId int
	var pendingWhileCondition string
	var pendingCriticalId int
	var pendingCriticalLockName string
	var pendingForEachId int
	var pendingForEachInputVal string
	var pendingForEachReturnVal string
	var pendingForEachUseSeparator bool
	var pendingForEachStartIndex string
	var pendingForEachEndIndex string
	var nextLoopId = 1
	var pendingIncludePath string

	// Counters and tracking arrays bool
	var inFloatProperty bool
	var floatPropName string
	var floatPropNameState bool
	var floatPropValueState bool

	var inJSONExtractor, inRegexExtractor bool
	var currentJSONExtractor *domain.JSONExtractor
	var currentRegexExtractor *domain.RegexExtractor

	var inResponseAssertion, inJSONAssertion bool
	var currentResponseAssertion *domain.ResponseAssertion
	var currentJSONAssertion *domain.JSONAssertion

	var inUltimateData bool
	var inUltimateRow bool
	var ultimateRowVals []string

	var inFreeFormData bool
	var inFreeFormRow bool
	var freeFormRowVals []string

	var currentCSVDataSet *domain.CSVDataSet
	var currentCookieManager *domain.CookieManager
	var currentCookie *domain.Cookie
	var currentCacheManager *domain.CacheManager
	var currentCounter *domain.Counter
	var currentDNSCacheManager *domain.DNSCacheManager
	var currentAuthManager *domain.AuthManager
	var currentAuthorization *domain.Authorization
	var currentRandomVariable *domain.RandomVariable
	var currentResultCollector *domain.ResultCollector
	var currentBackendListener *domain.BackendListener
	var currentThroughputTimer *domain.ThroughputTimer
	var inDNSServers bool
	var inDNSHosts bool
	var currentStaticHostName string

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
			if testNameAttr != "" && (currentTag == "HTTPSamplerProxy" || strings.HasSuffix(currentTag, "ThreadGroup") || currentTag == "ThroughputController" || currentTag == "TransactionController") {
				nameAttr = testNameAttr
			}

			if currentTag == "hashTree" {
				hashTreeDepth++
				if pendingWeight > 0 {
					weightMap[hashTreeDepth] = pendingWeight
					activeWeight = pendingWeight
					pendingWeight = 0
				}
				if pendingIfCondition != "" {
					ifConditionMap[hashTreeDepth] = pendingIfCondition
					pendingIfCondition = ""
				}
				if pendingTransactionName != "" {
					transactionNameMap[hashTreeDepth] = pendingTransactionName
					transactionParentMap[hashTreeDepth] = pendingTransactionParent
					pendingTransactionName = ""
					pendingTransactionParent = false
				}
				if pendingIncludePath != "" && currentThreadGroup != nil {
					incPath := pendingIncludePath
					pendingIncludePath = "" // reset

					// Resolve absolute path if relative
					if !filepath.IsAbs(incPath) && filePath != "" {
						incPath = filepath.Join(filepath.Dir(filePath), incPath)
					}

					// Recursively parse the external file
					subParser := NewDefaultJmxParser()
					subPlan, err := subParser.Parse(incPath)
					if err == nil && subPlan != nil {
						for _, tg := range subPlan.ThreadGroups {
							// Append samplers
							currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, tg.Samplers...)
							// Append configs
							currentThreadGroup.CSVDataSets = append(currentThreadGroup.CSVDataSets, tg.CSVDataSets...)
							currentThreadGroup.Timers = append(currentThreadGroup.Timers, tg.Timers...)
							currentThreadGroup.RandomVariables = append(currentThreadGroup.RandomVariables, tg.RandomVariables...)
						}
					}
				}
				if pendingLoopId > 0 && currentThreadGroup != nil {
					startIndex := len(currentThreadGroup.Samplers)
					loopStack = append(loopStack, LoopContext{
						Depth:      hashTreeDepth,
						LoopId:     pendingLoopId,
						StartIndex: startIndex,
						IsWhile:    false,
					})
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						IsControlFlow: true,
						ControlType:   "LoopStart",
						LoopId:        pendingLoopId,
						LoopCountExpr: pendingLoopCountExpr,
						LoopContinue:  pendingLoopContinue,
					})
					pendingLoopId = 0
					pendingLoopCountExpr = ""
					pendingLoopContinue = false
				}
				if pendingWhileId > 0 && currentThreadGroup != nil {
					startIndex := len(currentThreadGroup.Samplers)
					loopStack = append(loopStack, LoopContext{
						Depth:      hashTreeDepth,
						LoopId:     pendingWhileId,
						StartIndex: startIndex,
						IsWhile:    true,
					})
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						IsControlFlow:  true,
						ControlType:    "WhileStart",
						LoopId:         pendingWhileId,
						WhileCondition: pendingWhileCondition,
					})
					pendingWhileId = 0
					pendingWhileCondition = ""
				}
				if pendingCriticalId > 0 && currentThreadGroup != nil {
					startIndex := len(currentThreadGroup.Samplers)
					loopStack = append(loopStack, LoopContext{
						Depth:      hashTreeDepth,
						LoopId:     pendingCriticalId,
						StartIndex: startIndex,
						IsCritical: true,
					})
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						IsControlFlow:    true,
						ControlType:      "CriticalStart",
						LoopId:           pendingCriticalId,
						CriticalLockName: pendingCriticalLockName,
					})
					pendingCriticalId = 0
					pendingCriticalLockName = ""
				}
				if pendingForEachId > 0 && currentThreadGroup != nil {
					startIndex := len(currentThreadGroup.Samplers)
					loopStack = append(loopStack, LoopContext{
						Depth:      hashTreeDepth,
						LoopId:     pendingForEachId,
						StartIndex: startIndex,
						IsForEach:  true,
					})
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						IsControlFlow:       true,
						ControlType:         "ForEachStart",
						LoopId:              pendingForEachId,
						ForEachInputVal:     pendingForEachInputVal,
						ForEachReturnVal:    pendingForEachReturnVal,
						ForEachUseSeparator: pendingForEachUseSeparator,
						ForEachStartIndex:   pendingForEachStartIndex,
						ForEachEndIndex:     pendingForEachEndIndex,
					})
					pendingForEachId = 0
					pendingForEachInputVal = ""
					pendingForEachReturnVal = ""
					pendingForEachUseSeparator = true
					pendingForEachStartIndex = ""
					pendingForEachEndIndex = ""
				}
			}

			if strings.HasSuffix(currentTag, "ThreadGroup") || currentTag == "TestFragmentController" {
				actionType := "main"
				switch currentTag {
				case "SetupThreadGroup":
					actionType = "setup"
				case "PostThreadGroup":
					actionType = "teardown"
				}

				currentThreadGroup = &domain.ThreadGroup{
					Name:       nameAttr,
					ActionType: actionType,
				}
				switch currentTag {
				case "kg.apc.jmeter.threads.SteppingThreadGroup":
					currentThreadGroup.SteppingConfig = &domain.SteppingConfig{}
				case "com.blazemeter.jmeter.threads.concurrency.ConcurrencyThreadGroup":
					currentThreadGroup.ConcurrencyConfig = &domain.ConcurrencyConfig{}
				case "com.blazemeter.jmeter.threads.arrivals.ArrivalsThreadGroup":
					currentThreadGroup.ArrivalsConfig = &domain.ArrivalsConfig{}
				case "kg.apc.jmeter.threads.UltimateThreadGroup":
					currentThreadGroup.UltimateConfig = &domain.UltimateConfig{}
				case "com.blazemeter.jmeter.threads.arrivals.FreeFormArrivalsThreadGroup":
					currentThreadGroup.FreeFormArrivalsConfig = &domain.FreeFormArrivalsConfig{}
				}
				plan.ThreadGroups = append(plan.ThreadGroups, currentThreadGroup)
				lastCompletedReq = nil
			} else if currentTag == "HTTPSamplerProxy" {
				activeIfCondition := ""
				var conditions []string
				for d := 1; d <= hashTreeDepth; d++ {
					if cond, ok := ifConditionMap[d]; ok && cond != "" {
						conditions = append(conditions, cond)
					}
				}
				if len(conditions) > 0 {
					activeIfCondition = strings.Join(conditions, " && ")
				}

				activeTransactionName := ""
				activeTransactionParent := false
				for d := 1; d <= hashTreeDepth; d++ {
					if name, ok := transactionNameMap[d]; ok && name != "" {
						activeTransactionName = name
						activeTransactionParent = transactionParentMap[d]
					}
				}

				currentReq = &domain.RequestTemplate{
					Headers: make(map[string]string),
				}
				if currentThreadGroup != nil {
					currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
						Name:              nameAttr,
						Request:           currentReq,
						Weight:            activeWeight,
						IfCondition:       activeIfCondition,
						TransactionName:   activeTransactionName,
						TransactionParent: activeTransactionParent,
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
				case "DNSCacheManager.servers":
					inDNSServers = true
				case "DNSCacheManager.hosts":
					inDNSHosts = true
				case "UserParameters.names":
					userParamState = "names"
				case "UserParameters.thread_values":
					userParamState = "values"
				case "ultimatethreadgroupdata":
					inUltimateData = true
				case "arrivals_schedule":
					inFreeFormData = true
				default:
					if inUltimateData {
						inUltimateRow = true
						ultimateRowVals = []string{}
					} else if inFreeFormData {
						inFreeFormRow = true
						freeFormRowVals = []string{}
					} else if currentTag == "TransactionController" {
						pendingTransactionName = nameAttr
					}
				}
			} else if currentTag == "LoopController" {
				pendingLoopId = nextLoopId
				nextLoopId++
			} else if currentTag == "WhileController" {
				pendingWhileId = nextLoopId
				nextLoopId++
			} else if currentTag == "CriticalSectionController" {
				pendingCriticalId = nextLoopId
				nextLoopId++
			} else if currentTag == "ForeachController" {
				pendingForEachId = nextLoopId
				pendingForEachUseSeparator = true // Default to true
				nextLoopId++
			} else if currentTag == "FloatProperty" {
				inFloatProperty = true
				floatPropName = ""
			} else if currentTag == "name" && inFloatProperty {
				floatPropNameState = true
			} else if currentTag == "value" && inFloatProperty {
				floatPropValueState = true
			} else if currentTag == "doubleProp" {
				inFloatProperty = true
				floatPropName = ""
			} else if currentTag == "ConstantTimer" || currentTag == "UniformRandomTimer" || currentTag == "GaussianRandomTimer" || currentTag == "PoissonRandomTimer" || currentTag == "SyncTimer" {
				if enabledAttr != "false" {
					currentTimer = &domain.Timer{
						Type: currentTag,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.Timers = append(currentThreadGroup.Timers, currentTimer)
					}
				}
			} else if currentTag == "ConstantThroughputTimer" || currentTag == "PreciseThroughputTimer" {
				if enabledAttr != "false" {
					currentThroughputTimer = &domain.ThroughputTimer{
						Type: currentTag,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.ThroughputTimers = append(currentThreadGroup.ThroughputTimers, currentThroughputTimer)
					} else {
						plan.ThroughputTimers = append(plan.ThroughputTimers, currentThroughputTimer)
					}
				}
			} else if currentTag == "JSONPostProcessor" {
				inJSONExtractor = true
				currentJSONExtractor = &domain.JSONExtractor{}
			} else if currentTag == "RegexExtractor" {
				inRegexExtractor = true
				currentRegexExtractor = &domain.RegexExtractor{}
			} else if currentTag == "ResponseAssertion" {
				inResponseAssertion = true
				currentResponseAssertion = &domain.ResponseAssertion{
					Name: testNameAttr,
				}
			} else if currentTag == "JSONPathAssertion" {
				inJSONAssertion = true
				currentJSONAssertion = &domain.JSONAssertion{
					Name: testNameAttr,
				}
			} else if currentTag == "CSVDataSet" {
				if enabledAttr != "false" {
					currentCSVDataSet = &domain.CSVDataSet{
						Delimiter: ",",
						Recycle:   true,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.CSVDataSets = append(currentThreadGroup.CSVDataSets, currentCSVDataSet)
					} else {
						plan.CSVDataSets = append(plan.CSVDataSets, currentCSVDataSet)
					}
				}
			} else if currentTag == "ResultCollector" {
				if enabledAttr != "false" {
					currentResultCollector = &domain.ResultCollector{
						Name: testNameAttr,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.ResultCollectors = append(currentThreadGroup.ResultCollectors, currentResultCollector)
					} else {
						plan.ResultCollectors = append(plan.ResultCollectors, currentResultCollector)
					}
				}
			} else if currentTag == "BackendListener" {
				if enabledAttr != "false" {
					currentBackendListener = &domain.BackendListener{
						Name:      testNameAttr,
						Arguments: make(map[string]string),
					}
					if currentThreadGroup != nil {
						currentThreadGroup.BackendListeners = append(currentThreadGroup.BackendListeners, currentBackendListener)
					} else {
						plan.BackendListeners = append(plan.BackendListeners, currentBackendListener)
					}
				}
			} else if currentTag == "CookieManager" {
				if enabledAttr != "false" {
					currentCookieManager = &domain.CookieManager{}
					if currentThreadGroup != nil {
						currentThreadGroup.CookieManager = currentCookieManager
					} else {
						plan.CookieManager = currentCookieManager
					}
				}
			} else if currentTag == "CacheManager" {
				if enabledAttr != "false" {
					currentCacheManager = &domain.CacheManager{
						MaxSize: 5000,
					}
					if currentThreadGroup != nil {
						currentThreadGroup.CacheManager = currentCacheManager
					} else {
						plan.CacheManager = currentCacheManager
					}
				}
			} else if currentTag == "CounterConfig" {
				if enabledAttr != "false" {
					currentCounter = &domain.Counter{
						Start: "0",
						Incr:  "1",
					}
					if currentThreadGroup != nil {
						currentThreadGroup.Counters = append(currentThreadGroup.Counters, currentCounter)
					} else {
						plan.Counters = append(plan.Counters, currentCounter)
					}
				}
			} else if currentTag == "DNSCacheManager" {
				if enabledAttr != "false" {
					currentDNSCacheManager = &domain.DNSCacheManager{
						Hosts: make(map[string]string),
					}
					if currentThreadGroup != nil {
						currentThreadGroup.DNSCacheManager = currentDNSCacheManager
					} else {
						plan.DNSCacheManager = currentDNSCacheManager
					}
				}
			} else if currentTag == "AuthManager" {
				if enabledAttr != "false" {
					currentAuthManager = &domain.AuthManager{}
					if currentThreadGroup != nil {
						currentThreadGroup.AuthManager = currentAuthManager
					} else {
						plan.AuthManager = currentAuthManager
					}
				}
			} else if currentTag == "RandomVariableConfig" {
				if enabledAttr != "false" {
					currentRandomVariable = &domain.RandomVariable{
						MinimumValue: "1",
						MaximumValue: "100",
					}
					if currentThreadGroup != nil {
						currentThreadGroup.RandomVariables = append(currentThreadGroup.RandomVariables, currentRandomVariable)
					} else {
						plan.RandomVariables = append(plan.RandomVariables, currentRandomVariable)
					}
				}
			} else if currentTag == "elementProp" && currentCookieManager != nil {
				var isCookie bool
				for _, attr := range se.Attr {
					if attr.Name.Local == "elementType" && attr.Value == "Cookie" {
						isCookie = true
					}
				}
				if isCookie {
					currentCookie = &domain.Cookie{Name: nameAttr}
				}
			} else if currentTag == "elementProp" && currentAuthManager != nil {
				var isAuth bool
				for _, attr := range se.Attr {
					if attr.Name.Local == "elementType" && attr.Value == "Authorization" {
						isAuth = true
					}
				}
				if isAuth {
					currentAuthorization = &domain.Authorization{}
				}
			}

		case xml.EndElement:
			if se.Name.Local == "hashTree" {
				// Handle LoopEnd / WhileEnd
				if len(loopStack) > 0 && currentThreadGroup != nil {
					top := loopStack[len(loopStack)-1]
					if top.Depth == hashTreeDepth {
						loopStack = loopStack[:len(loopStack)-1]

						endType := "LoopEnd"
						if top.IsWhile {
							endType = "WhileEnd"
						} else if top.IsCritical {
							endType = "CriticalEnd"
						} else if top.IsForEach {
							endType = "ForEachEnd"
						}

						currentThreadGroup.Samplers = append(currentThreadGroup.Samplers, &domain.Sampler{
							IsControlFlow: true,
							ControlType:   endType,
							LoopId:        top.LoopId,
							LoopJumpIndex: top.StartIndex,
						})

						if top.IsWhile {
							// Update WhileStart's JumpIndex to point to the WhileEnd
							currentThreadGroup.Samplers[top.StartIndex].LoopJumpIndex = len(currentThreadGroup.Samplers) - 1
						} else if top.IsCritical {
							// CriticalEnd needs the lock name, we copy it from CriticalStart
							currentThreadGroup.Samplers[len(currentThreadGroup.Samplers)-1].CriticalLockName = currentThreadGroup.Samplers[top.StartIndex].CriticalLockName
						} else if top.IsForEach {
							// Update ForEachStart's JumpIndex to point to the ForEachEnd
							currentThreadGroup.Samplers[top.StartIndex].LoopJumpIndex = len(currentThreadGroup.Samplers) - 1
						}
					}
				}

				delete(weightMap, hashTreeDepth)
				delete(ifConditionMap, hashTreeDepth)
				delete(transactionNameMap, hashTreeDepth)
				delete(transactionParentMap, hashTreeDepth)
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
			} else if se.Name.Local == "FloatProperty" || se.Name.Local == "doubleProp" {
				inFloatProperty = false
				floatPropName = ""
			} else if se.Name.Local == "name" && inFloatProperty {
				floatPropNameState = false
			} else if se.Name.Local == "value" && inFloatProperty {
				floatPropValueState = false
			} else if se.Name.Local == "ConstantTimer" || se.Name.Local == "UniformRandomTimer" || se.Name.Local == "GaussianRandomTimer" || se.Name.Local == "PoissonRandomTimer" || se.Name.Local == "SyncTimer" {
				currentTimer = nil
			} else if se.Name.Local == "ConstantThroughputTimer" || se.Name.Local == "PreciseThroughputTimer" {
				currentThroughputTimer = nil
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
							currentRegexExtractor.MatchNo,
						))
					}
				}
				currentRegexExtractor = nil
			} else if se.Name.Local == "ResponseAssertion" {
				inResponseAssertion = false
				if currentResponseAssertion != nil && currentThreadGroup != nil {
					lastSamplerIdx := len(currentThreadGroup.Samplers) - 1
					if lastSamplerIdx >= 0 {
						lastSampler := currentThreadGroup.Samplers[lastSamplerIdx]
						lastSampler.Assertions = append(lastSampler.Assertions, currentResponseAssertion)
					} else {
						currentThreadGroup.Assertions = append(currentThreadGroup.Assertions, currentResponseAssertion)
					}
				}
				currentResponseAssertion = nil
			} else if se.Name.Local == "JSONPathAssertion" {
				inJSONAssertion = false
				if currentJSONAssertion != nil && currentThreadGroup != nil {
					lastSamplerIdx := len(currentThreadGroup.Samplers) - 1
					if lastSamplerIdx >= 0 {
						lastSampler := currentThreadGroup.Samplers[lastSamplerIdx]
						lastSampler.Assertions = append(lastSampler.Assertions, currentJSONAssertion)
					} else {
						currentThreadGroup.Assertions = append(currentThreadGroup.Assertions, currentJSONAssertion)
					}
				}
				currentJSONAssertion = nil
			} else if se.Name.Local == "CSVDataSet" {
				currentCSVDataSet = nil
			} else if se.Name.Local == "CookieManager" {
				currentCookieManager = nil
			} else if se.Name.Local == "CacheManager" {
				currentCacheManager = nil
			} else if se.Name.Local == "CounterConfig" {
				currentCounter = nil
			} else if se.Name.Local == "DNSCacheManager" {
				currentDNSCacheManager = nil
			} else if se.Name.Local == "AuthManager" {
				currentAuthManager = nil
			} else if se.Name.Local == "RandomVariableConfig" {
				currentRandomVariable = nil
			} else if se.Name.Local == "ResultCollector" {
				currentResultCollector = nil
			} else if se.Name.Local == "BackendListener" {
				currentBackendListener = nil
			} else if se.Name.Local == "elementProp" && currentCookie != nil {
				if currentCookieManager != nil {
					currentCookieManager.Cookies = append(currentCookieManager.Cookies, *currentCookie)
				}
				currentCookie = nil
			} else if se.Name.Local == "elementProp" && currentAuthorization != nil {
				if currentAuthManager != nil {
					currentAuthManager.AuthList = append(currentAuthManager.AuthList, *currentAuthorization)
				}
				currentAuthorization = nil
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
				inDNSServers = false
				inDNSHosts = false
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
				} else if inFreeFormRow {
					inFreeFormRow = false
					if currentThreadGroup != nil && currentThreadGroup.FreeFormArrivalsConfig != nil && len(freeFormRowVals) >= 3 {
						currentThreadGroup.FreeFormArrivalsConfig.Schedule = append(currentThreadGroup.FreeFormArrivalsConfig.Schedule, domain.FreeFormScheduleItem{
							Start:    freeFormRowVals[0],
							End:      freeFormRowVals[1],
							Duration: freeFormRowVals[2],
						})
					}
				} else if inFreeFormData {
					inFreeFormData = false
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
					switch floatPropName {
					case "ThroughputController.percentThroughput":
						if v, err := strconv.ParseFloat(val, 64); err == nil {
							pendingWeight = v
						}
					case "throughput":
						if currentThroughputTimer != nil {
							currentThroughputTimer.Throughput = val
						}
					}
				}
			case "boolProp":
				if nameAttr == "LoopController.continue_forever" {
					pendingLoopContinue = (val == "true")
				}
				if nameAttr == "ForeachController.useSeparator" {
					pendingForEachUseSeparator = (val == "true" || val == "") // default true
				}
				if nameAttr == "HTTPSampler.postBodyRaw" && val == "true" {
					postBodyRaw = true
				}
				if currentCSVDataSet != nil {
					switch nameAttr {
					case "ignoreFirstLine":
						currentCSVDataSet.IgnoreFirstLine = (val == "true")
					case "quotedData":
						currentCSVDataSet.QuotedData = (val == "true")
					case "recycle":
						currentCSVDataSet.Recycle = (val == "true" || val == "") // default true in JMeter
					case "stopThread":
						currentCSVDataSet.StopThread = (val == "true")
					}
				}
				if currentCookieManager != nil {
					switch nameAttr {
					case "CookieManager.clearEachIteration":
						currentCookieManager.ClearEachIteration = (val == "true")
					case "CookieManager.controlledByThread":
						currentCookieManager.ControlledByThread = (val == "true")
					}
				}
				if currentJSONAssertion != nil {
					switch nameAttr {
					case "JSONVALIDATION":
						currentJSONAssertion.JSONValidation = (val == "true")
					case "EXPECT_NULL":
						currentJSONAssertion.ExpectNull = (val == "true")
					case "INVERT":
						currentJSONAssertion.Invert = (val == "true")
					case "ISREGEX":
						currentJSONAssertion.IsRegex = (val == "true")
					}
				}
				if currentCookie != nil && nameAttr == "Cookie.secure" {
					currentCookie.Secure = (val == "true")
				}
				if currentCacheManager != nil {
					switch nameAttr {
					case "clearEachIteration":
						currentCacheManager.ClearEachIteration = (val == "true")
					case "useExpires":
						currentCacheManager.UseExpires = (val == "true")
					}
				}
				if currentCounter != nil && nameAttr == "CounterConfig.per_user" {
					currentCounter.PerUser = (val == "true")
				}
				if currentDNSCacheManager != nil {
					switch nameAttr {
					case "DNSCacheManager.clearEachIteration":
						currentDNSCacheManager.ClearEachIteration = (val == "true")
					case "DNSCacheManager.isCustomResolver":
						currentDNSCacheManager.IsCustomResolver = (val == "true")
					}
				}
				if currentAuthManager != nil && nameAttr == "AuthManager.clearEachIteration" {
					currentAuthManager.ClearEachIteration = (val == "true")
				}
				if currentRandomVariable != nil && nameAttr == "perThread" {
					currentRandomVariable.PerThread = (val == "true")
				}
				if currentResultCollector != nil && nameAttr == "ResultCollector.error_logging" {
					currentResultCollector.ErrorLogging = (val == "true")
				}
			case "intProp":
				if currentCacheManager != nil && nameAttr == "maxSize" {
					if v, err := strconv.Atoi(val); err == nil {
						currentCacheManager.MaxSize = v
					}
				}
				if currentTimer != nil && currentTimer.Type == "SyncTimer" {
					if nameAttr == "groupSize" {
						currentTimer.GroupSize = val
					}
				}
				if currentResponseAssertion != nil && nameAttr == "Assertion.test_type" {
					if v, err := strconv.Atoi(val); err == nil {
						currentResponseAssertion.TestType = v
					}
				}
			case "longProp":
				if currentTimer != nil && currentTimer.Type == "SyncTimer" {
					if nameAttr == "timeoutInMs" {
						currentTimer.TimeoutInMs = val
					}
				}
			case "stringProp":
				if inUltimateRow {
					ultimateRowVals = append(ultimateRowVals, val)
					continue
				}
				if inFreeFormRow {
					freeFormRowVals = append(freeFormRowVals, val)
					continue
				}
				switch nameAttr {

				case "IfController.condition":
					pendingIfCondition = val
				case "TransactionController.parent":
					pendingTransactionParent = (val == "true")
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
				case "timeoutInMs":
					if currentTimer != nil && currentTimer.Type == "SyncTimer" {
						currentTimer.TimeoutInMs = val
					}
				case "Assertion.custom_message":
					if currentResponseAssertion != nil {
						currentResponseAssertion.CustomFailure = val
					}
				case "Assertion.test_field":
					if currentResponseAssertion != nil {
						currentResponseAssertion.TestField = val
					}
				case "JSON_PATH":
					if currentJSONAssertion != nil {
						currentJSONAssertion.JSONPath = val
					}
				case "EXPECTED_VALUE":
					if currentJSONAssertion != nil {
						currentJSONAssertion.ExpectedValue = val
					}
				case "throughput":
					if currentThroughputTimer != nil {
						currentThroughputTimer.Throughput = val
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
					} else if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.TargetLevel = val
					}
				case "RampUp":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.RampUp = val
					} else if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.RampUp = val
					}
				case "Steps":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Steps = val
					} else if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.Steps = val
					}
				case "Hold":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Hold = val
					} else if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.Hold = val
					}
				case "Unit":
					if currentThreadGroup != nil && currentThreadGroup.ConcurrencyConfig != nil {
						currentThreadGroup.ConcurrencyConfig.Unit = val
					} else if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.Unit = val
					} else if currentThreadGroup != nil && currentThreadGroup.FreeFormArrivalsConfig != nil {
						currentThreadGroup.FreeFormArrivalsConfig.Unit = val
					}
				case "ConcurrencyLimit":
					if currentThreadGroup != nil && currentThreadGroup.ArrivalsConfig != nil {
						currentThreadGroup.ArrivalsConfig.ConcurrencyLimit = val
					} else if currentThreadGroup != nil && currentThreadGroup.FreeFormArrivalsConfig != nil {
						currentThreadGroup.FreeFormArrivalsConfig.ConcurrencyLimit = val
					}
				case "ThroughputController.maxThroughput":
					if v, err := strconv.ParseFloat(val, 64); err == nil {
						pendingWeight = v
					}
				case "IncludeController.includepath":
					pendingIncludePath = val
				case "Argument.name":
					currentArgName = val
				case "LoopController.loops":
					pendingLoopCountExpr = val
				case "WhileController.condition":
					pendingWhileCondition = val
				case "CriticalSectionController.lockName":
					pendingCriticalLockName = val
				case "ForeachController.inputVal":
					pendingForEachInputVal = val
				case "ForeachController.returnVal":
					pendingForEachReturnVal = val
				case "ForeachController.startIndex":
					pendingForEachStartIndex = val
				case "ForeachController.endIndex":
					pendingForEachEndIndex = val
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
				case "JSONPostProcessor.match_numbers":
					if inJSONExtractor && currentJSONExtractor != nil {
						if num, err := strconv.Atoi(val); err == nil {
							currentJSONExtractor.MatchNo = num
						}
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
				case "RegexExtractor.match_number":
					if inRegexExtractor && currentRegexExtractor != nil {
						if num, err := strconv.Atoi(val); err == nil {
							currentRegexExtractor.MatchNo = num
						}
					}
					// Removed misplaced default block
				case "filename":
					if currentCSVDataSet != nil {
						currentCSVDataSet.Filename = val
					}
					if currentResultCollector != nil {
						currentResultCollector.Filename = val
					}
				case "fileEncoding":
					if currentCSVDataSet != nil {
						currentCSVDataSet.FileEncoding = val
					}
				case "variableNames":
					if currentCSVDataSet != nil {
						currentCSVDataSet.VariableNames = val
					}
				case "delimiter":
					if currentCSVDataSet != nil {
						currentCSVDataSet.Delimiter = val
					}
				case "shareMode":
					if currentCSVDataSet != nil {
						currentCSVDataSet.ShareMode = val
					}
				case "Cookie.value":
					if currentCookie != nil {
						currentCookie.Value = val
					}
				case "Cookie.domain":
					if currentCookie != nil {
						currentCookie.Domain = val
					}
				case "Cookie.path":
					if currentCookie != nil {
						currentCookie.Path = val
					}
				case "CounterConfig.start":
					if currentCounter != nil {
						currentCounter.Start = val
					}
				case "CounterConfig.end":
					if currentCounter != nil {
						currentCounter.End = val
					}
				case "CounterConfig.incr":
					if currentCounter != nil {
						currentCounter.Incr = val
					}
				case "CounterConfig.name":
					if currentCounter != nil {
						currentCounter.Name = val
					}
				case "CounterConfig.format":
					if currentCounter != nil {
						currentCounter.Format = val
					}
				case "StaticHost.Name":
					if currentDNSCacheManager != nil && inDNSHosts {
						currentStaticHostName = val
					}
				case "StaticHost.Address":
					if currentDNSCacheManager != nil && inDNSHosts && currentStaticHostName != "" {
						currentDNSCacheManager.Hosts[currentStaticHostName] = val
						currentStaticHostName = ""
					}
				case "Authorization.url":
					if currentAuthorization != nil {
						currentAuthorization.URL = val
					}
				case "Authorization.username":
					if currentAuthorization != nil {
						currentAuthorization.Username = val
					}
				case "Authorization.password":
					if currentAuthorization != nil {
						currentAuthorization.Password = val
					}
				case "Authorization.mechanism":
					if currentAuthorization != nil {
						currentAuthorization.Mechanism = val
					}
				case "maximumValue":
					if currentRandomVariable != nil {
						currentRandomVariable.MaximumValue = val
					}
				case "minimumValue":
					if currentRandomVariable != nil {
						currentRandomVariable.MinimumValue = val
					}
				case "outputFormat":
					if currentRandomVariable != nil {
						currentRandomVariable.Format = val
					}
				case "randomSeed":
					if currentRandomVariable != nil {
						currentRandomVariable.RandomSeed = val
					}
				case "variableName":
					if currentRandomVariable != nil {
						currentRandomVariable.Name = val
					}
				case "classname":
					if currentBackendListener != nil {
						currentBackendListener.Classname = val
					}
				default:
					if inResponseAssertion && currentResponseAssertion != nil {
						if !strings.HasPrefix(nameAttr, "Assertion.") {
							currentResponseAssertion.TestStrings = append(currentResponseAssertion.TestStrings, val)
						}
					}
					_ = inJSONAssertion

					if inDNSServers && currentDNSCacheManager != nil {
						currentDNSCacheManager.Servers = append(currentDNSCacheManager.Servers, val)
					}
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
