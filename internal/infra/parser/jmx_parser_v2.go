package parser

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xvlet/vjm/internal/domain"
)

// LoopContext tracks active loops and controller blocks during XML traversal.
type LoopContext struct {
	Depth         int
	LoopId        int
	StartIndex    int
	IsWhile       bool
	IsCritical    bool
	IsForEach     bool
	IsInterleave  bool
	IsOnceOnly    bool
	IsRandom      bool
	IsRandomOrder bool
	IsRuntime     bool
	IsThroughput  bool
}

// JmxParserV2 is the refactored, state-machine based parser for JMeter JMX files.
type JmxParserV2 struct {
	filePath string
	plan     *domain.TestPlan
	decoder  *xml.Decoder

	currentThreadGroup        *domain.ThreadGroup
	lastCompletedReq          *domain.RequestTemplate
	lastSamplerHashTreeDepth  int
	expectingSamplerChildTree bool
	currentTimer              *domain.Timer

	currentTag        string
	nameAttr          string
	testNameAttr      string
	enabledAttr       string
	currentHeaderName string
	currentArgName    string
	currentArgValue   string

	isArgumentProp      bool
	inHeaderManager     bool
	postBodyRaw         bool
	inConfigTestElement bool

	currentReq  *domain.RequestTemplate
	domainVal   string
	portVal     string
	pathVal     string
	protocolVal string
	defDomain   string
	defPort     string
	defPath     string
	defProtocol string

	graphQLQuery         string
	graphQLVariables     string
	graphQLOperationName string

	currentLogFile      string
	currentTestAction   *domain.Sampler
	currentDebugSampler *domain.Sampler

	inUserParameters bool
	userParamState   string
	userParamNames   []string
	userParamValues  []string

	inSystemSamplerArguments      bool
	inSystemSamplerEnvironment    bool
	inResultCollectorObjPropValue bool
	inMainControllerElementProp   bool

	hashTreeDepth int
	pendingWeight float64
	activeWeight  float64
	weightMap     map[int]float64

	pendingIfCondition string
	ifConditionMap     map[int]string

	pendingTransactionName   string
	pendingTransactionParent bool
	transactionNameMap       map[int]string
	transactionParentMap     map[int]bool

	loopStack                   []LoopContext
	pendingLoopId               int
	pendingLoopCountExpr        string
	pendingLoopContinue         bool
	pendingWhileId              int
	pendingWhileCondition       string
	pendingCriticalId           int
	pendingCriticalLockName     string
	pendingForEachId            int
	pendingForEachInputVal      string
	pendingForEachReturnVal     string
	pendingForEachUseSeparator  bool
	pendingForEachStartIndex    string
	pendingForEachEndIndex      string
	pendingInterleaveId         int
	pendingOnceOnlyId           int
	pendingRandomId             int
	pendingRandomOrderId        int
	pendingRuntimeId            int
	pendingRuntimeSeconds       string
	pendingSwitchId             int
	pendingSwitchValue          string
	pendingModuleId             int
	inModuleNodePath            bool
	pendingModuleTargetNodePath []string
	nextLoopId                  int
	pendingIncludePath          string

	activeExtractors    map[int][]domain.Extractor
	activeAssertions    map[int][]domain.Assertion
	activePreProcessors map[int][]domain.PreProcessor
	firstSamplerAtDepth map[int]int

	inFloatProperty     bool
	floatPropName       string
	floatPropNameState  bool
	floatPropValueState bool

	inJSONExtractor       bool
	inRegexExtractor      bool
	currentJSONExtractor  *domain.JSONExtractor
	currentRegexExtractor *domain.RegexExtractor

	inResponseAssertion      bool
	inJSONAssertion          bool
	inSizeAssertion          bool
	inXPathAssertion         bool
	inCompareAssertion       bool
	inDurationAssertion      bool
	inMD5HexAssertion        bool
	inSMIMEAssertion         bool
	inXMLAssertion           bool
	inHTMLLinkParser         bool
	currentResponseAssertion *domain.ResponseAssertion
	currentJSONAssertion     *domain.JSONAssertion
	currentSizeAssertion     *domain.SizeAssertion
	currentXPathAssertion    *domain.XPathAssertion
	currentCompareAssertion  *domain.CompareAssertion
	currentDurationAssertion *domain.DurationAssertion
	currentMD5HexAssertion   *domain.MD5HexAssertion
	currentSMIMEAssertion    *domain.SMIMEAssertion
	currentXMLAssertion      *domain.XMLAssertion
	currentHTMLLinkParser    *domain.HTMLLinkParser

	inURLRewritingModifier      bool
	currentURLRewritingModifier *domain.URLRewritingModifier
	inRegExUserParameters       bool
	currentRegExUserParameters  *domain.RegExUserParameters
	inSampleTimeout             bool
	currentSampleTimeout        *domain.SampleTimeout
	inHtmlExtractor             bool
	currentHtmlExtractor        *domain.HtmlExtractor
	inJMESPathExtractor         bool
	currentJMESPathExtractor    *domain.JMESPathExtractor
	inBoundaryExtractor         bool
	currentBoundaryExtractor    *domain.BoundaryExtractor
	inDebugPostProcessor        bool
	currentDebugPostProcessor   *domain.DebugPostProcessor
	inResultAction              bool
	currentResultAction         *domain.ResultAction
	inXPathExtractor            bool
	currentXPathExtractor       *domain.XPathExtractor
	currentResultSaver          *domain.ResultSaver

	inUltimateData  bool
	inUltimateRow   bool
	ultimateRowVals []string

	inFreeFormData  bool
	inFreeFormRow   bool
	freeFormRowVals []string

	pendingThroughputId        int
	pendingThroughputStyle     int
	pendingThroughputMax       string
	pendingThroughputPerThread bool

	currentCSVDataSet      *domain.CSVDataSet
	currentCookieManager   *domain.CookieManager
	currentCookie          *domain.Cookie
	currentCacheManager    *domain.CacheManager
	currentCounter         *domain.Counter
	currentDNSCacheManager *domain.DNSCacheManager
	currentAuthManager     *domain.AuthManager
	currentAuthorization   *domain.Authorization
	currentRandomVariable  *domain.RandomVariable
	currentResultCollector *domain.ResultCollector
	currentBackendListener *domain.BackendListener
	currentThroughputTimer *domain.ThroughputTimer
	inDNSServers           bool
	inDNSHosts             bool
	currentStaticHostName  string
}

// NewJmxParserV2 initializes a new JmxParserV2 instance.
func NewJmxParserV2() *JmxParserV2 {
	return &JmxParserV2{
		activeWeight:             1.0,
		weightMap:                make(map[int]float64),
		ifConditionMap:           make(map[int]string),
		transactionNameMap:       make(map[int]string),
		transactionParentMap:     make(map[int]bool),
		activeExtractors:         make(map[int][]domain.Extractor),
		activeAssertions:         make(map[int][]domain.Assertion),
		activePreProcessors:      make(map[int][]domain.PreProcessor),
		firstSamplerAtDepth:      make(map[int]int),
		nextLoopId:               1,
		lastSamplerHashTreeDepth: -1,
	}
}

// Parse parses a JMeter .jmx file and returns a domain.TestPlan.
func (p *JmxParserV2) Parse(filePath string) (*domain.TestPlan, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	p.filePath = filePath
	p.decoder = xml.NewDecoder(file)
	p.plan = &domain.TestPlan{
		UserDefinedVariables: make(map[string]string),
	}

	for {
		t, err := p.decoder.Token()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("xml parse error: %w", err)
		}
		if t == nil {
			break
		}

		switch se := t.(type) {
		case xml.StartElement:
			p.handleStartElement(se)
		case xml.EndElement:
			p.handleEndElement(se)
		case xml.CharData:
			p.handleCharData(se)
		}
	}

	p.postProcessControllers()

	return p.plan, nil
}

func (p *JmxParserV2) appendSamplers(samplers ...*domain.Sampler) {
	if p.currentThreadGroup == nil {
		return
	}
	for _, sampler := range samplers {
		if !sampler.IsControlFlow {
			for d := 1; d <= p.hashTreeDepth; d++ {
				if exts, ok := p.activeExtractors[d]; ok {
					sampler.Extractors = append(sampler.Extractors, exts...)
				}
				if asts, ok := p.activeAssertions[d]; ok {
					sampler.Assertions = append(sampler.Assertions, asts...)
				}
				if pres, ok := p.activePreProcessors[d]; ok {
					sampler.PreProcessors = append(sampler.PreProcessors, pres...)
				}
			}
		}
		p.currentThreadGroup.Samplers = append(p.currentThreadGroup.Samplers, sampler)
	}
}

func (p *JmxParserV2) getActiveIfCondition() string {
	var conditions []string
	for d := 1; d <= p.hashTreeDepth; d++ {
		if cond, ok := p.ifConditionMap[d]; ok && cond != "" {
			conditions = append(conditions, cond)
		}
	}
	if len(conditions) > 0 {
		return strings.Join(conditions, " && ")
	}
	return ""
}

func (p *JmxParserV2) getActiveTransaction() (string, bool) {
	activeTransactionName := ""
	activeTransactionParent := false
	for d := 1; d <= p.hashTreeDepth; d++ {
		if name, ok := p.transactionNameMap[d]; ok && name != "" {
			activeTransactionName = name
			activeTransactionParent = p.transactionParentMap[d]
		}
	}
	return activeTransactionName, activeTransactionParent
}

func (p *JmxParserV2) handleStartElement(se xml.StartElement) {
	p.currentTag = se.Name.Local
	switch p.currentTag {
	case "InterleaveControl":
		p.pendingInterleaveId = p.nextLoopId
		p.nextLoopId++
	case "OnceOnlyController":
		p.pendingOnceOnlyId = p.nextLoopId
		p.nextLoopId++
	case "RandomController":
		p.pendingRandomId = p.nextLoopId
		p.nextLoopId++
	case "RandomOrderController":
		p.pendingRandomOrderId = p.nextLoopId
		p.nextLoopId++
	}

	p.nameAttr = ""
	p.testNameAttr = ""
	p.enabledAttr = "true"
	for _, attr := range se.Attr {
		switch attr.Name.Local {
		case "name":
			p.nameAttr = attr.Value
		case "testname":
			p.testNameAttr = attr.Value
		case "enabled":
			p.enabledAttr = attr.Value
		}
	}

	if p.testNameAttr != "" && (p.currentTag == "HTTPSamplerProxy" || p.currentTag == "GraphQLHTTPSamplerProxy" || p.currentTag == "TestAction" || p.currentTag == "DebugSampler" || p.currentTag == "AccessLogSampler" || p.currentTag == "SystemSampler" || strings.HasSuffix(p.currentTag, "ThreadGroup") || p.currentTag == "ThroughputController" || p.currentTag == "TransactionController" || p.currentTag == "TestFragmentController" || p.currentTag == "TestPlan" || strings.Contains(p.currentTag, "SSESampler")) {
		p.nameAttr = p.testNameAttr
	}

	switch {
	case p.currentTag == "hashTree":
		p.startHashTree()
	case p.currentTag == "TestPlan":
		p.plan.Name = p.nameAttr
	case strings.HasSuffix(p.currentTag, "ThreadGroup") || p.currentTag == "TestFragmentController":
		p.startThreadGroup()
	case p.currentTag == "DebugSampler":
		p.startDebugSampler()
	case p.currentTag == "AccessLogSampler":
		p.startAccessLogSampler()
	case p.currentTag == "SystemSampler":
		p.startSystemSampler()
	case p.currentTag == "TestAction":
		p.startTestAction()
	case p.currentTag == "HTTPSamplerProxy" || p.currentTag == "GraphQLHTTPSamplerProxy" || strings.Contains(p.currentTag, "WebSocketSampler") || strings.Contains(p.currentTag, "SSESampler"):
		p.startHttpOrNetworkSampler()
	case p.currentTag == "HeaderManager" || p.currentTag == "ConfigTestElement" || p.currentTag == "UserParameters" || p.currentTag == "CSVDataSet" || p.currentTag == "ResultCollector" || p.currentTag == "MailerResultCollector" || p.currentTag == "ResultSaver" || p.currentTag == "BackendListener" || p.currentTag == "Summariser" || p.currentTag == "CookieManager" || p.currentTag == "CacheManager" || p.currentTag == "CounterConfig" || p.currentTag == "DNSCacheManager" || p.currentTag == "AuthManager" || p.currentTag == "RandomVariableConfig" || (p.currentTag == "value" && p.currentResultCollector != nil):
		p.startConfigOrListener(se)
	case p.currentTag == "collectionProp":
		p.startCollectionProp()
	case p.currentTag == "LoopController" || p.currentTag == "WhileController" || p.currentTag == "CriticalSectionController" || p.currentTag == "ForeachController" || p.currentTag == "RecordingController" || p.currentTag == "GenericController" || p.currentTag == "RunTime" || p.currentTag == "ModuleController" || p.currentTag == "ThroughputController" || p.currentTag == "SwitchController":
		p.startController()
	case p.currentTag == "FloatProperty" || p.currentTag == "doubleProp" || p.currentTag == "name" || p.currentTag == "value":
		p.startFloatProperty()
	case p.currentTag == "ConstantTimer" || p.currentTag == "UniformRandomTimer" || p.currentTag == "GaussianRandomTimer" || p.currentTag == "PoissonRandomTimer" || p.currentTag == "SyncTimer" || p.currentTag == "ConstantThroughputTimer" || p.currentTag == "PreciseThroughputTimer":
		p.startTimer()
	case p.currentTag == "JSONPostProcessor" || p.currentTag == "RegexExtractor" || p.currentTag == "HtmlExtractor" || p.currentTag == "JMESPathExtractor" || p.currentTag == "BoundaryExtractor" || p.currentTag == "DebugPostProcessor" || p.currentTag == "ResultAction" || p.currentTag == "XPathExtractor":
		p.startExtractor()
	case p.currentTag == "ResponseAssertion" || p.currentTag == "JSONPathAssertion" || p.currentTag == "SizeAssertion" || p.currentTag == "XPathAssertion" || p.currentTag == "CompareAssertion" || p.currentTag == "DurationAssertion" || p.currentTag == "MD5HexAssertion" || p.currentTag == "SMIMEAssertion" || p.currentTag == "XMLAssertion":
		p.startAssertion()
	case p.currentTag == "HTMLLinkParser" || p.currentTag == "URLRewritingModifier" || p.currentTag == "RegExUserParameters" || p.currentTag == "SampleTimeout" || p.currentTag == "org.apache.jmeter.modifiers.SampleTimeout":
		p.startPreProcessor()
	case p.currentTag == "elementProp":
		p.startElementProp(se)
	}
}

func (p *JmxParserV2) startHashTree() {
	p.hashTreeDepth++
	if p.currentThreadGroup != nil {
		p.firstSamplerAtDepth[p.hashTreeDepth] = len(p.currentThreadGroup.Samplers)
	}
	if p.expectingSamplerChildTree {
		p.lastSamplerHashTreeDepth = p.hashTreeDepth
		p.expectingSamplerChildTree = false
	}
	if p.pendingWeight > 0 {
		p.weightMap[p.hashTreeDepth] = p.pendingWeight
		p.activeWeight = p.pendingWeight
		p.pendingWeight = 0
	}
	if p.pendingIfCondition != "" {
		p.ifConditionMap[p.hashTreeDepth] = p.pendingIfCondition
		p.pendingIfCondition = ""
	}
	if p.pendingTransactionName != "" {
		p.transactionNameMap[p.hashTreeDepth] = p.pendingTransactionName
		p.transactionParentMap[p.hashTreeDepth] = p.pendingTransactionParent
		p.pendingTransactionName = ""
		p.pendingTransactionParent = false
	}
	if p.pendingIncludePath != "" && p.currentThreadGroup != nil {
		incPath := p.pendingIncludePath
		p.pendingIncludePath = ""

		if !filepath.IsAbs(incPath) && p.filePath != "" {
			incPath = filepath.Join(filepath.Dir(p.filePath), incPath)
		}

		subParser := NewDefaultJmxParser()
		subPlan, err := subParser.Parse(incPath)
		if err == nil && subPlan != nil {
			for _, tg := range subPlan.ThreadGroups {
				offset := len(p.currentThreadGroup.Samplers)
				if offset > 0 {
					for _, s := range tg.Samplers {
						if !s.IsControlFlow {
							continue
						}
						s.LoopJumpIndex += offset
						s.BlockEndIndex += offset
						for i := range s.RandomChildStarts {
							s.RandomChildStarts[i] += offset
						}
						for i := range s.RandomChildEnds {
							s.RandomChildEnds[i] += offset
						}
						for i := range s.RandomOrderChildStarts {
							s.RandomOrderChildStarts[i] += offset
						}
						for i := range s.RandomOrderChildEnds {
							s.RandomOrderChildEnds[i] += offset
						}
						for i := range s.InterleaveChildStarts {
							s.InterleaveChildStarts[i] += offset
						}
						for i := range s.InterleaveChildEnds {
							s.InterleaveChildEnds[i] += offset
						}
						for i := range s.SwitchChildStarts {
							s.SwitchChildStarts[i] += offset
						}
						for i := range s.SwitchChildEnds {
							s.SwitchChildEnds[i] += offset
						}
					}
				}
				p.appendSamplers(tg.Samplers...)
				p.currentThreadGroup.CSVDataSets = append(p.currentThreadGroup.CSVDataSets, tg.CSVDataSets...)
				p.currentThreadGroup.Timers = append(p.currentThreadGroup.Timers, tg.Timers...)
				p.currentThreadGroup.RandomVariables = append(p.currentThreadGroup.RandomVariables, tg.RandomVariables...)
			}
		}
	}
	if p.pendingLoopId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingLoopId,
			StartIndex: startIndex,
			IsWhile:    false,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow: true,
			ControlType:   "LoopStart",
			LoopId:        p.pendingLoopId,
			LoopCountExpr: p.pendingLoopCountExpr,
			LoopContinue:  p.pendingLoopContinue,
		})
		p.pendingLoopId = 0
		p.pendingLoopCountExpr = ""
		p.pendingLoopContinue = false
	}
	if p.pendingWhileId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingWhileId,
			StartIndex: startIndex,
			IsWhile:    true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:  true,
			ControlType:    "WhileStart",
			LoopId:         p.pendingWhileId,
			WhileCondition: p.pendingWhileCondition,
		})
		p.pendingWhileId = 0
		p.pendingWhileCondition = ""
	}
	if p.pendingRuntimeId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingRuntimeId,
			StartIndex: startIndex,
			IsRuntime:  true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:      true,
			ControlType:        "RuntimeStart",
			LoopId:             p.pendingRuntimeId,
			RuntimeSecondsExpr: p.pendingRuntimeSeconds,
		})
		p.pendingRuntimeId = 0
		p.pendingRuntimeSeconds = ""
	}
	if p.pendingModuleId > 0 && p.currentThreadGroup != nil {
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:        true,
			ControlType:          "ModuleCall",
			LoopId:               p.pendingModuleId,
			ModuleTargetNodePath: p.pendingModuleTargetNodePath,
		})
		p.pendingModuleId = 0
		p.pendingModuleTargetNodePath = nil
	}
	if p.pendingThroughputId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:        p.hashTreeDepth,
			LoopId:       p.pendingThroughputId,
			StartIndex:   startIndex,
			IsThroughput: true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:       true,
			ControlType:         "ThroughputStart",
			LoopId:              p.pendingThroughputId,
			ThroughputStyle:     p.pendingThroughputStyle,
			ThroughputMaxExpr:   p.pendingThroughputMax,
			ThroughputPerThread: p.pendingThroughputPerThread,
		})
		p.pendingThroughputId = 0
		p.pendingThroughputStyle = 0
		p.pendingThroughputMax = ""
		p.pendingThroughputPerThread = false
	}
	if p.pendingSwitchId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingSwitchId,
			StartIndex: startIndex,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:   true,
			ControlType:     "SwitchStart",
			LoopId:          p.pendingSwitchId,
			SwitchValueExpr: p.pendingSwitchValue,
		})
		p.pendingSwitchId = 0
		p.pendingSwitchValue = ""
	}
	if p.pendingCriticalId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingCriticalId,
			StartIndex: startIndex,
			IsCritical: true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:    true,
			ControlType:      "CriticalStart",
			LoopId:           p.pendingCriticalId,
			CriticalLockName: p.pendingCriticalLockName,
		})
		p.pendingCriticalId = 0
		p.pendingCriticalLockName = ""
	}
	if p.pendingForEachId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingForEachId,
			StartIndex: startIndex,
			IsForEach:  true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow:       true,
			ControlType:         "ForEachStart",
			LoopId:              p.pendingForEachId,
			ForEachInputVal:     p.pendingForEachInputVal,
			ForEachReturnVal:    p.pendingForEachReturnVal,
			ForEachUseSeparator: p.pendingForEachUseSeparator,
			ForEachStartIndex:   p.pendingForEachStartIndex,
			ForEachEndIndex:     p.pendingForEachEndIndex,
		})
		p.pendingForEachId = 0
		p.pendingForEachInputVal = ""
		p.pendingForEachReturnVal = ""
		p.pendingForEachUseSeparator = true
		p.pendingForEachStartIndex = ""
		p.pendingForEachEndIndex = ""
	}
	if p.pendingInterleaveId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:        p.hashTreeDepth,
			LoopId:       p.pendingInterleaveId,
			StartIndex:   startIndex,
			IsInterleave: true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow: true,
			ControlType:   "InterleaveStart",
			LoopId:        p.pendingInterleaveId,
		})
		p.pendingInterleaveId = 0
	}
	if p.pendingOnceOnlyId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingOnceOnlyId,
			StartIndex: startIndex,
			IsOnceOnly: true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow: true,
			ControlType:   "OnceOnlyStart",
			LoopId:        p.pendingOnceOnlyId,
		})
		p.pendingOnceOnlyId = 0
	}
	if p.pendingRandomId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:      p.hashTreeDepth,
			LoopId:     p.pendingRandomId,
			StartIndex: startIndex,
			IsRandom:   true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow: true,
			ControlType:   "RandomStart",
			LoopId:        p.pendingRandomId,
		})
		p.pendingRandomId = 0
	}
	if p.pendingRandomOrderId > 0 && p.currentThreadGroup != nil {
		startIndex := len(p.currentThreadGroup.Samplers)
		p.loopStack = append(p.loopStack, LoopContext{
			Depth:         p.hashTreeDepth,
			LoopId:        p.pendingRandomOrderId,
			StartIndex:    startIndex,
			IsRandomOrder: true,
		})
		p.appendSamplers(&domain.Sampler{
			IsControlFlow: true,
			ControlType:   "RandomOrderStart",
			LoopId:        p.pendingRandomOrderId,
		})
		p.pendingRandomOrderId = 0
	}
}

func (p *JmxParserV2) startThreadGroup() {
	actionType := "main"
	switch p.currentTag {
	case "SetupThreadGroup":
		actionType = "setup"
	case "PostThreadGroup":
		actionType = "teardown"
	case "TestFragmentController":
		actionType = "fragment"
	}

	p.currentThreadGroup = &domain.ThreadGroup{
		Name:       p.nameAttr,
		ActionType: actionType,
	}
	switch p.currentTag {
	case "kg.apc.jmeter.threads.SteppingThreadGroup":
		p.currentThreadGroup.SteppingConfig = &domain.SteppingConfig{}
	case "com.blazemeter.jmeter.threads.concurrency.ConcurrencyThreadGroup":
		p.currentThreadGroup.ConcurrencyConfig = &domain.ConcurrencyConfig{}
	case "com.blazemeter.jmeter.threads.arrivals.ArrivalsThreadGroup":
		p.currentThreadGroup.ArrivalsConfig = &domain.ArrivalsConfig{}
	case "kg.apc.jmeter.threads.UltimateThreadGroup":
		p.currentThreadGroup.UltimateConfig = &domain.UltimateConfig{}
	case "com.blazemeter.jmeter.threads.arrivals.FreeFormArrivalsThreadGroup":
		p.currentThreadGroup.FreeFormArrivalsConfig = &domain.FreeFormArrivalsConfig{}
	}
	if p.enabledAttr != "false" || p.currentTag == "TestFragmentController" {
		p.plan.ThreadGroups = append(p.plan.ThreadGroups, p.currentThreadGroup)
	}
	p.lastCompletedReq = nil
}

func (p *JmxParserV2) startDebugSampler() {
	activeIfCondition := p.getActiveIfCondition()
	activeTransactionName, activeTransactionParent := p.getActiveTransaction()

	p.currentDebugSampler = &domain.Sampler{
		Name:              p.nameAttr,
		IsControlFlow:     true,
		ControlType:       "DebugSampler",
		Weight:            p.activeWeight,
		IfCondition:       activeIfCondition,
		TransactionName:   activeTransactionName,
		TransactionParent: activeTransactionParent,
	}
	if p.currentThreadGroup != nil {
		p.appendSamplers(p.currentDebugSampler)
	}
}

func (p *JmxParserV2) startAccessLogSampler() {
	activeIfCondition := p.getActiveIfCondition()
	activeTransactionName, activeTransactionParent := p.getActiveTransaction()

	p.currentReq = &domain.RequestTemplate{
		Headers: make(map[string]string),
	}
	sampler := &domain.Sampler{
		Name:               p.nameAttr,
		Request:            p.currentReq,
		Weight:             p.activeWeight,
		IfCondition:        activeIfCondition,
		TransactionName:    activeTransactionName,
		TransactionParent:  activeTransactionParent,
		IsAccessLogSampler: true,
	}
	if p.currentThreadGroup != nil {
		p.appendSamplers(sampler)
	}
	p.domainVal, p.portVal, p.pathVal, p.protocolVal, p.currentLogFile = "", "", "", "", ""
	p.currentHeaderName = ""
	p.postBodyRaw = false
}

func (p *JmxParserV2) startSystemSampler() {
	activeIfCondition := p.getActiveIfCondition()
	activeTransactionName, activeTransactionParent := p.getActiveTransaction()

	p.currentReq = &domain.RequestTemplate{
		Headers: make(map[string]string),
	}
	sampler := &domain.Sampler{
		Name:               p.nameAttr,
		Request:            p.currentReq,
		Weight:             p.activeWeight,
		IfCondition:        activeIfCondition,
		TransactionName:    activeTransactionName,
		TransactionParent:  activeTransactionParent,
		IsOSProcessSampler: true,
		OSEnvironment:      make(map[string]string),
	}
	if p.currentThreadGroup != nil {
		p.appendSamplers(sampler)
	}
}

func (p *JmxParserV2) startTestAction() {
	activeIfCondition := p.getActiveIfCondition()
	activeTransactionName, activeTransactionParent := p.getActiveTransaction()

	p.currentTestAction = &domain.Sampler{
		Name:              p.nameAttr,
		IsControlFlow:     true,
		ControlType:       "TestAction",
		Weight:            p.activeWeight,
		IfCondition:       activeIfCondition,
		TransactionName:   activeTransactionName,
		TransactionParent: activeTransactionParent,
	}
	if p.currentThreadGroup != nil {
		p.appendSamplers(p.currentTestAction)
	}
}

func (p *JmxParserV2) startHttpOrNetworkSampler() {
	activeIfCondition := p.getActiveIfCondition()
	activeTransactionName, activeTransactionParent := p.getActiveTransaction()

	p.currentReq = &domain.RequestTemplate{
		Headers: make(map[string]string),
	}
	if p.currentThreadGroup != nil {
		p.appendSamplers(&domain.Sampler{
			Name:              p.nameAttr,
			Request:           p.currentReq,
			Weight:            p.activeWeight,
			IfCondition:       activeIfCondition,
			TransactionName:   activeTransactionName,
			TransactionParent: activeTransactionParent,
			IsSSESampler:      strings.Contains(p.currentTag, "SSESampler"),
		})
	}
	p.domainVal, p.portVal, p.pathVal, p.protocolVal, p.currentLogFile = "", "", "", "", ""
	p.currentHeaderName = ""
	p.postBodyRaw = false
}

func (p *JmxParserV2) startCollectionProp() {
	switch p.nameAttr {
	case "DNSCacheManager.servers":
		p.inDNSServers = true
	case "DNSCacheManager.hosts":
		p.inDNSHosts = true
	case "UserParameters.names":
		p.userParamState = "names"
	case "UserParameters.thread_values":
		p.userParamState = "values"
	case "ultimatethreadgroupdata":
		p.inUltimateData = true
	case "Schedule", "arrivals_schedule":
		p.inFreeFormData = true
	case "ModuleController.node_path":
		p.inModuleNodePath = true
	default:
		if p.inUltimateData {
			p.inUltimateRow = true
			p.ultimateRowVals = []string{}
		} else if p.inFreeFormData {
			p.inFreeFormRow = true
			p.freeFormRowVals = []string{}
		} else if p.currentTag == "TransactionController" {
			p.pendingTransactionName = p.nameAttr
		}
	}
}

func (p *JmxParserV2) startController() {
	switch p.currentTag {
	case "LoopController":
		p.pendingLoopId = p.nextLoopId
		p.nextLoopId++
	case "WhileController":
		p.pendingWhileId = p.nextLoopId
		p.nextLoopId++
	case "CriticalSectionController":
		p.pendingCriticalId = p.nextLoopId
		p.nextLoopId++
	case "ForeachController":
		p.pendingForEachId = p.nextLoopId
		p.pendingForEachUseSeparator = true
		p.nextLoopId++
	case "RecordingController", "GenericController":
		// Transparent containers
	case "RunTime":
		p.pendingRuntimeId = p.nextLoopId
		p.nextLoopId++
	case "ModuleController":
		p.pendingModuleId = p.nextLoopId
		p.nextLoopId++
		p.pendingModuleTargetNodePath = []string{}
	case "ThroughputController":
		p.pendingThroughputId = p.nextLoopId
		p.nextLoopId++
	case "SwitchController":
		p.pendingSwitchId = p.nextLoopId
		p.nextLoopId++
	}
}

func (p *JmxParserV2) startFloatProperty() {
	switch p.currentTag {
	case "OnceOnlyController", "FloatProperty", "doubleProp":
		p.inFloatProperty = true
		p.floatPropName = ""
	case "name":
		if p.inFloatProperty {
			p.floatPropNameState = true
		}
	case "value":
		if p.inFloatProperty {
			p.floatPropValueState = true
		}
	}
}

func (p *JmxParserV2) startTimer() {
	switch p.currentTag {
	case "ConstantTimer", "UniformRandomTimer", "GaussianRandomTimer", "PoissonRandomTimer", "SyncTimer":
		if p.enabledAttr != "false" {
			p.currentTimer = &domain.Timer{
				Type: p.currentTag,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.Timers = append(p.currentThreadGroup.Timers, p.currentTimer)
			}
		}
	case "ConstantThroughputTimer", "PreciseThroughputTimer":
		if p.enabledAttr != "false" {
			p.currentThroughputTimer = &domain.ThroughputTimer{
				Type: p.currentTag,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.ThroughputTimers = append(p.currentThreadGroup.ThroughputTimers, p.currentThroughputTimer)
			} else {
				p.plan.ThroughputTimers = append(p.plan.ThroughputTimers, p.currentThroughputTimer)
			}
		}
	}
}

func (p *JmxParserV2) startExtractor() {
	switch p.currentTag {
	case "JSONPostProcessor":
		p.inJSONExtractor = true
		p.currentJSONExtractor = &domain.JSONExtractor{}
	case "RegexExtractor":
		p.inRegexExtractor = true
		p.currentRegexExtractor = &domain.RegexExtractor{}
	case "HtmlExtractor":
		p.inHtmlExtractor = true
		p.currentHtmlExtractor = &domain.HtmlExtractor{
			MatchNo: 1,
		}
	case "JMESPathExtractor":
		p.inJMESPathExtractor = true
		p.currentJMESPathExtractor = &domain.JMESPathExtractor{
			MatchNo: 1,
		}
	case "BoundaryExtractor":
		p.inBoundaryExtractor = true
		p.currentBoundaryExtractor = &domain.BoundaryExtractor{
			MatchNo: 1,
		}
	case "DebugPostProcessor":
		p.inDebugPostProcessor = true
		p.currentDebugPostProcessor = &domain.DebugPostProcessor{
			Name: p.testNameAttr,
		}
	case "ResultAction":
		p.inResultAction = true
		p.currentResultAction = &domain.ResultAction{
			Action: 0,
		}
	case "XPathExtractor":
		p.inXPathExtractor = true
		p.currentXPathExtractor = &domain.XPathExtractor{
			MatchNumber: -1,
		}
	}
}

func (p *JmxParserV2) startAssertion() {
	switch p.currentTag {
	case "ResponseAssertion":
		p.inResponseAssertion = true
		p.currentResponseAssertion = &domain.ResponseAssertion{
			Name: p.testNameAttr,
		}
	case "JSONPathAssertion":
		p.inJSONAssertion = true
		p.currentJSONAssertion = &domain.JSONAssertion{
			Name: p.testNameAttr,
		}
	case "SizeAssertion":
		p.inSizeAssertion = true
		p.currentSizeAssertion = &domain.SizeAssertion{
			Name: p.testNameAttr,
		}
	case "XPathAssertion":
		p.inXPathAssertion = true
		p.currentXPathAssertion = &domain.XPathAssertion{
			Name: p.testNameAttr,
		}
	case "CompareAssertion":
		p.inCompareAssertion = true
		p.currentCompareAssertion = &domain.CompareAssertion{
			Name: p.testNameAttr,
		}
	case "DurationAssertion":
		p.inDurationAssertion = true
		p.currentDurationAssertion = &domain.DurationAssertion{
			Name: p.testNameAttr,
		}
	case "MD5HexAssertion":
		p.inMD5HexAssertion = true
		p.currentMD5HexAssertion = &domain.MD5HexAssertion{
			Name: p.testNameAttr,
		}
	case "SMIMEAssertion":
		p.inSMIMEAssertion = true
		p.currentSMIMEAssertion = &domain.SMIMEAssertion{
			Name: p.testNameAttr,
		}
	case "XMLAssertion":
		p.inXMLAssertion = true
		p.currentXMLAssertion = &domain.XMLAssertion{
			Name: p.testNameAttr,
		}
	}
}

func (p *JmxParserV2) startPreProcessor() {
	switch p.currentTag {
	case "HTMLLinkParser":
		p.inHTMLLinkParser = true
		p.currentHTMLLinkParser = &domain.HTMLLinkParser{
			Name: p.testNameAttr,
		}
	case "URLRewritingModifier":
		p.inURLRewritingModifier = true
		p.currentURLRewritingModifier = &domain.URLRewritingModifier{
			Name: p.testNameAttr,
		}
	case "RegExUserParameters":
		p.inRegExUserParameters = true
		p.currentRegExUserParameters = &domain.RegExUserParameters{
			Name:            p.testNameAttr,
			ParamNamesGrNr:  "1",
			ParamValuesGrNr: "2",
		}
	case "SampleTimeout", "org.apache.jmeter.modifiers.SampleTimeout":
		p.inSampleTimeout = true
		p.currentSampleTimeout = &domain.SampleTimeout{
			Name: p.testNameAttr,
		}
	}
}

func (p *JmxParserV2) startConfigOrListener(se xml.StartElement) {
	switch p.currentTag {
	case "HeaderManager":
		p.inHeaderManager = true
	case "ConfigTestElement":
		p.inConfigTestElement = true
	case "UserParameters":
		p.inUserParameters = true
		p.userParamNames = []string{}
		p.userParamValues = []string{}
	case "CSVDataSet":
		if p.enabledAttr != "false" {
			p.currentCSVDataSet = &domain.CSVDataSet{
				Delimiter: ",",
				Recycle:   true,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.CSVDataSets = append(p.currentThreadGroup.CSVDataSets, p.currentCSVDataSet)
			} else {
				p.plan.CSVDataSets = append(p.plan.CSVDataSets, p.currentCSVDataSet)
			}
		}
	case "ResultCollector", "MailerResultCollector":
		if p.enabledAttr != "false" {
			p.currentResultCollector = &domain.ResultCollector{
				Name: p.testNameAttr,
			}
			if p.currentTag == "MailerResultCollector" {
				p.currentResultCollector.MailerModel = &domain.MailerModel{}
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.ResultCollectors = append(p.currentThreadGroup.ResultCollectors, p.currentResultCollector)
			} else {
				p.plan.ResultCollectors = append(p.plan.ResultCollectors, p.currentResultCollector)
			}
		}
	case "value":
		if p.currentResultCollector != nil {
			for _, attr := range se.Attr {
				if attr.Name.Local == "class" && attr.Value == "SampleSaveConfiguration" {
					p.inResultCollectorObjPropValue = true
				}
			}
		}
	case "ResultSaver":
		if p.enabledAttr != "false" {
			p.currentResultSaver = &domain.ResultSaver{
				Name: p.testNameAttr,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.ResultSavers = append(p.currentThreadGroup.ResultSavers, p.currentResultSaver)
			} else {
				p.plan.ResultSavers = append(p.plan.ResultSavers, p.currentResultSaver)
			}
		}
	case "BackendListener":
		if p.enabledAttr != "false" {
			p.currentBackendListener = &domain.BackendListener{
				Name:      p.testNameAttr,
				Arguments: make(map[string]string),
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.BackendListeners = append(p.currentThreadGroup.BackendListeners, p.currentBackendListener)
			} else {
				p.plan.BackendListeners = append(p.plan.BackendListeners, p.currentBackendListener)
			}
		}
	case "Summariser":
		if p.enabledAttr != "false" {
			sum := &domain.Summariser{
				Name: p.testNameAttr,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.Summarisers = append(p.currentThreadGroup.Summarisers, sum)
			} else {
				p.plan.Summarisers = append(p.plan.Summarisers, sum)
			}
		}
	case "CookieManager":
		if p.enabledAttr != "false" {
			p.currentCookieManager = &domain.CookieManager{}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.CookieManager = p.currentCookieManager
			} else {
				p.plan.CookieManager = p.currentCookieManager
			}
		}
	case "CacheManager":
		if p.enabledAttr != "false" {
			p.currentCacheManager = &domain.CacheManager{
				MaxSize: 5000,
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.CacheManager = p.currentCacheManager
			} else {
				p.plan.CacheManager = p.currentCacheManager
			}
		}
	case "CounterConfig":
		if p.enabledAttr != "false" {
			p.currentCounter = &domain.Counter{
				Start: "0",
				Incr:  "1",
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.Counters = append(p.currentThreadGroup.Counters, p.currentCounter)
			} else {
				p.plan.Counters = append(p.plan.Counters, p.currentCounter)
			}
		}
	case "DNSCacheManager":
		if p.enabledAttr != "false" {
			p.currentDNSCacheManager = &domain.DNSCacheManager{
				Hosts: make(map[string]string),
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.DNSCacheManager = p.currentDNSCacheManager
			} else {
				p.plan.DNSCacheManager = p.currentDNSCacheManager
			}
		}
	case "AuthManager":
		if p.enabledAttr != "false" {
			p.currentAuthManager = &domain.AuthManager{}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.AuthManager = p.currentAuthManager
			} else {
				p.plan.AuthManager = p.currentAuthManager
			}
		}
	case "RandomVariableConfig":
		if p.enabledAttr != "false" {
			p.currentRandomVariable = &domain.RandomVariable{
				MinimumValue: "1",
				MaximumValue: "100",
			}
			if p.currentThreadGroup != nil {
				p.currentThreadGroup.RandomVariables = append(p.currentThreadGroup.RandomVariables, p.currentRandomVariable)
			} else {
				p.plan.RandomVariables = append(p.plan.RandomVariables, p.currentRandomVariable)
			}
		}
	}
}

func (p *JmxParserV2) startElementProp(se xml.StartElement) {
	if p.currentCookieManager != nil {
		var isCookie bool
		for _, attr := range se.Attr {
			if attr.Name.Local == "elementType" && attr.Value == "Cookie" {
				isCookie = true
			}
		}
		if isCookie {
			p.currentCookie = &domain.Cookie{Name: p.nameAttr}
			return
		}
	}

	if p.currentAuthManager != nil {
		var isAuth bool
		for _, attr := range se.Attr {
			if attr.Name.Local == "elementType" && attr.Value == "Authorization" {
				isAuth = true
			}
		}
		if isAuth {
			p.currentAuthorization = &domain.Authorization{}
			return
		}
	}

	if p.nameAttr == "ThreadGroup.main_controller" {
		p.inMainControllerElementProp = true
		return
	}
	if p.nameAttr == "SystemSampler.arguments" {
		p.inSystemSamplerArguments = true
		return
	}
	if p.nameAttr == "SystemSampler.environment" {
		p.inSystemSamplerEnvironment = true
		return
	}

	var isArg bool
	for _, attr := range se.Attr {
		if attr.Name.Local == "elementType" && (attr.Value == "HTTPArgument" || attr.Value == "Argument") {
			isArg = true
			break
		}
	}
	if isArg {
		p.isArgumentProp = true
		p.currentArgName = ""
		p.currentArgValue = ""
	}
}

func (p *JmxParserV2) handleEndElement(ee xml.EndElement) {
	tag := ee.Name.Local
	switch {
	case tag == "hashTree":
		p.endHashTree()
	case tag == "HeaderManager" || tag == "ConfigTestElement" || tag == "CSVDataSet" || tag == "CookieManager" || tag == "CacheManager" || tag == "CounterConfig" || tag == "DNSCacheManager" || tag == "AuthManager" || tag == "RandomVariableConfig" || tag == "ResultCollector" || tag == "MailerResultCollector" || tag == "ResultSaver" || tag == "BackendListener" || (tag == "value" && p.inResultCollectorObjPropValue):
		p.endConfigOrListener(tag)
	case tag == "FloatProperty" || tag == "doubleProp" || (tag == "name" && p.inFloatProperty) || (tag == "value" && p.inFloatProperty):
		p.endFloatProperty(tag)
	case tag == "ConstantTimer" || tag == "UniformRandomTimer" || tag == "GaussianRandomTimer" || tag == "PoissonRandomTimer" || tag == "SyncTimer" || tag == "ConstantThroughputTimer" || tag == "PreciseThroughputTimer":
		p.endTimer(tag)
	case tag == "JSONPostProcessor" || tag == "RegexExtractor" || tag == "HtmlExtractor" || tag == "JMESPathExtractor" || tag == "BoundaryExtractor" || tag == "DebugPostProcessor" || tag == "ResultAction" || tag == "XPathExtractor":
		p.endExtractor(tag)
	case tag == "ResponseAssertion" || tag == "JSONPathAssertion" || tag == "SizeAssertion" || tag == "XPathAssertion" || tag == "CompareAssertion" || tag == "DurationAssertion" || tag == "MD5HexAssertion" || tag == "SMIMEAssertion" || tag == "XMLAssertion":
		p.endAssertion(tag)
	case tag == "HTMLLinkParser" || tag == "URLRewritingModifier" || tag == "RegExUserParameters" || tag == "SampleTimeout" || tag == "org.apache.jmeter.modifiers.SampleTimeout":
		p.endPreProcessor(tag)
	case tag == "elementProp":
		p.endElementProp()
	case tag == "AccessLogSampler" || tag == "SystemSampler" || tag == "DebugSampler" || tag == "TestAction" || tag == "HTTPSamplerProxy" || tag == "GraphQLHTTPSamplerProxy" || strings.Contains(tag, "WebSocketSampler") || strings.Contains(tag, "SSESampler"):
		p.endSampler(tag)
	case tag == "UserParameters":
		p.endUserParameters()
	case tag == "collectionProp":
		p.endCollectionProp()
	}
}

func (p *JmxParserV2) endHashTree() {
	if len(p.loopStack) > 0 && p.currentThreadGroup != nil {
		top := p.loopStack[len(p.loopStack)-1]
		if top.Depth == p.hashTreeDepth {
			p.loopStack = p.loopStack[:len(p.loopStack)-1]

			endType := "LoopEnd"
			if top.IsWhile {
				endType = "WhileEnd"
			} else if top.IsCritical {
				endType = "CriticalEnd"
			} else if top.IsForEach {
				endType = "ForEachEnd"
			} else if top.IsInterleave {
				endType = "InterleaveEnd"
			} else if top.IsOnceOnly {
				endType = "OnceOnlyEnd"
			} else if top.IsRandom {
				endType = "RandomEnd"
			} else if top.IsRandomOrder {
				endType = "RandomOrderEnd"
			} else if top.IsRuntime {
				endType = "RuntimeEnd"
			} else if top.IsThroughput {
				endType = "ThroughputEnd"
			}

			p.appendSamplers(&domain.Sampler{
				IsControlFlow: true,
				ControlType:   endType,
				LoopId:        top.LoopId,
				LoopJumpIndex: top.StartIndex,
			})

			if top.IsWhile {
				p.currentThreadGroup.Samplers[top.StartIndex].LoopJumpIndex = len(p.currentThreadGroup.Samplers) - 1
			} else if top.IsCritical {
				p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1].CriticalLockName = p.currentThreadGroup.Samplers[top.StartIndex].CriticalLockName
			} else if top.IsForEach {
				p.currentThreadGroup.Samplers[top.StartIndex].LoopJumpIndex = len(p.currentThreadGroup.Samplers) - 1
			} else if top.IsRuntime {
				p.currentThreadGroup.Samplers[top.StartIndex].LoopJumpIndex = len(p.currentThreadGroup.Samplers) - 1
			}

			p.currentThreadGroup.Samplers[top.StartIndex].BlockEndIndex = len(p.currentThreadGroup.Samplers) - 1
		}
	}

	delete(p.weightMap, p.hashTreeDepth)
	delete(p.ifConditionMap, p.hashTreeDepth)
	delete(p.transactionNameMap, p.hashTreeDepth)
	delete(p.transactionParentMap, p.hashTreeDepth)
	delete(p.activeExtractors, p.hashTreeDepth)
	delete(p.activeAssertions, p.hashTreeDepth)
	delete(p.activePreProcessors, p.hashTreeDepth)
	delete(p.firstSamplerAtDepth, p.hashTreeDepth)
	p.hashTreeDepth--
	if p.hashTreeDepth == 2 {
		p.currentThreadGroup = nil
	}
	p.activeWeight = 1.0
	for d := p.hashTreeDepth; d >= 0; d-- {
		if w, ok := p.weightMap[d]; ok {
			p.activeWeight = w
			break
		}
	}
}

func (p *JmxParserV2) endConfigOrListener(tag string) {
	switch tag {
	case "HeaderManager":
		p.inHeaderManager = false
	case "ConfigTestElement":
		p.inConfigTestElement = false
	case "CSVDataSet":
		p.currentCSVDataSet = nil
	case "CookieManager":
		p.currentCookieManager = nil
	case "CacheManager":
		p.currentCacheManager = nil
	case "CounterConfig":
		p.currentCounter = nil
	case "DNSCacheManager":
		p.currentDNSCacheManager = nil
	case "AuthManager":
		p.currentAuthManager = nil
	case "RandomVariableConfig":
		p.currentRandomVariable = nil
	case "ResultCollector", "MailerResultCollector":
		p.currentResultCollector = nil
	case "value":
		p.inResultCollectorObjPropValue = false
	case "ResultSaver":
		p.currentResultSaver = nil
	case "BackendListener":
		p.currentBackendListener = nil
	}
}

func (p *JmxParserV2) endFloatProperty(tag string) {
	switch tag {
	case "FloatProperty", "doubleProp":
		p.inFloatProperty = false
		p.floatPropName = ""
	case "name":
		p.floatPropNameState = false
	case "value":
		p.floatPropValueState = false
	}
}

func (p *JmxParserV2) endTimer(tag string) {
	switch tag {
	case "ConstantTimer", "UniformRandomTimer", "GaussianRandomTimer", "PoissonRandomTimer", "SyncTimer":
		p.currentTimer = nil
	case "ConstantThroughputTimer", "PreciseThroughputTimer":
		p.currentThroughputTimer = nil
	}
}

func (p *JmxParserV2) endExtractor(tag string) {
	switch tag {
	case "JSONPostProcessor":
		p.inJSONExtractor = false
		if p.currentJSONExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentJSONExtractor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentJSONExtractor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentJSONExtractor)
						}
					}
				}
			}
		}
		p.currentJSONExtractor = nil

	case "RegexExtractor":
		p.inRegexExtractor = false
		if p.currentRegexExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, domain.NewRegexExtractor(
					p.currentRegexExtractor.ReferenceName,
					p.currentRegexExtractor.Regex,
					p.currentRegexExtractor.Template,
					p.currentRegexExtractor.DefaultValueStr,
					p.currentRegexExtractor.MatchNo,
				))
			}
		}
		p.currentRegexExtractor = nil

	case "HtmlExtractor":
		p.inHtmlExtractor = false
		if p.currentHtmlExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentHtmlExtractor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentHtmlExtractor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentHtmlExtractor)
						}
					}
				}
			}
		}
		p.currentHtmlExtractor = nil

	case "JMESPathExtractor":
		p.inJMESPathExtractor = false
		if p.currentJMESPathExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentJMESPathExtractor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentJMESPathExtractor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentJMESPathExtractor)
						}
					}
				}
			}
		}
		p.currentJMESPathExtractor = nil

	case "BoundaryExtractor":
		p.inBoundaryExtractor = false
		if p.currentBoundaryExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentBoundaryExtractor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentBoundaryExtractor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentBoundaryExtractor)
						}
					}
				}
			}
		}
		p.currentBoundaryExtractor = nil

	case "DebugPostProcessor":
		p.inDebugPostProcessor = false
		if p.currentDebugPostProcessor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentDebugPostProcessor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentDebugPostProcessor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentDebugPostProcessor)
						}
					}
				}
			}
		}
		p.currentDebugPostProcessor = nil

	case "ResultAction":
		p.inResultAction = false
		if p.currentResultAction != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentResultAction)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentResultAction)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentResultAction)
						}
					}
				}
			}
		}
		p.currentResultAction = nil

	case "XPathExtractor":
		p.inXPathExtractor = false
		if p.currentXPathExtractor != nil && p.currentThreadGroup != nil {
			if len(p.currentThreadGroup.Samplers) > 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				lastSampler.Extractors = append(lastSampler.Extractors, p.currentXPathExtractor)
			} else {
				p.activeExtractors[p.hashTreeDepth] = append(p.activeExtractors[p.hashTreeDepth], p.currentXPathExtractor)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Extractors = append(p.currentThreadGroup.Samplers[i].Extractors, p.currentXPathExtractor)
						}
					}
				}
			}
		}
		p.currentXPathExtractor = nil
	}
}

func (p *JmxParserV2) endAssertion(tag string) {
	switch tag {
	case "ResponseAssertion":
		p.inResponseAssertion = false
		if p.currentResponseAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentResponseAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentResponseAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentResponseAssertion)
						}
					}
				}
			}
		}
		p.currentResponseAssertion = nil

	case "JSONPathAssertion":
		p.inJSONAssertion = false
		if p.currentJSONAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentJSONAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentJSONAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentJSONAssertion)
						}
					}
				}
			}
		}
		p.currentJSONAssertion = nil

	case "SizeAssertion":
		p.inSizeAssertion = false
		if p.currentSizeAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentSizeAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentSizeAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentSizeAssertion)
						}
					}
				}
			}
		}
		p.currentSizeAssertion = nil

	case "XPathAssertion":
		p.inXPathAssertion = false
		if p.currentXPathAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentXPathAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentXPathAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentXPathAssertion)
						}
					}
				}
			}
		}
		p.currentXPathAssertion = nil

	case "CompareAssertion":
		p.inCompareAssertion = false
		if p.currentCompareAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentCompareAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentCompareAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentCompareAssertion)
						}
					}
				}
			}
		}
		p.currentCompareAssertion = nil

	case "DurationAssertion":
		p.inDurationAssertion = false
		if p.currentDurationAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentDurationAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentDurationAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentDurationAssertion)
						}
					}
				}
			}
		}
		p.currentDurationAssertion = nil

	case "MD5HexAssertion":
		p.inMD5HexAssertion = false
		if p.currentMD5HexAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentMD5HexAssertion)
			} else {
				p.currentThreadGroup.Assertions = append(p.currentThreadGroup.Assertions, p.currentMD5HexAssertion)
			}
		}
		p.currentMD5HexAssertion = nil

	case "SMIMEAssertion":
		p.inSMIMEAssertion = false
		if p.currentSMIMEAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentSMIMEAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentSMIMEAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentSMIMEAssertion)
						}
					}
				}
			}
		}
		p.currentSMIMEAssertion = nil

	case "XMLAssertion":
		p.inXMLAssertion = false
		if p.currentXMLAssertion != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 && p.hashTreeDepth == p.lastSamplerHashTreeDepth {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.Assertions = append(lastSampler.Assertions, p.currentXMLAssertion)
			} else {
				p.activeAssertions[p.hashTreeDepth] = append(p.activeAssertions[p.hashTreeDepth], p.currentXMLAssertion)
				if startIdx, ok := p.firstSamplerAtDepth[p.hashTreeDepth]; ok {
					for i := startIdx; i < len(p.currentThreadGroup.Samplers); i++ {
						if !p.currentThreadGroup.Samplers[i].IsControlFlow {
							p.currentThreadGroup.Samplers[i].Assertions = append(p.currentThreadGroup.Samplers[i].Assertions, p.currentXMLAssertion)
						}
					}
				}
			}
		}
		p.currentXMLAssertion = nil
	}
}

func (p *JmxParserV2) endPreProcessor(tag string) {
	switch tag {
	case "HTMLLinkParser":
		p.inHTMLLinkParser = false
		if p.currentHTMLLinkParser != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.PreProcessors = append(lastSampler.PreProcessors, p.currentHTMLLinkParser)
			}
		}
		p.currentHTMLLinkParser = nil

	case "URLRewritingModifier":
		p.inURLRewritingModifier = false
		if p.currentURLRewritingModifier != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.PreProcessors = append(lastSampler.PreProcessors, p.currentURLRewritingModifier)
			}
		}
		p.currentURLRewritingModifier = nil

	case "RegExUserParameters":
		p.inRegExUserParameters = false
		if p.currentRegExUserParameters != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.PreProcessors = append(lastSampler.PreProcessors, p.currentRegExUserParameters)
			}
		}
		p.currentRegExUserParameters = nil

	case "SampleTimeout", "org.apache.jmeter.modifiers.SampleTimeout":
		p.inSampleTimeout = false
		if p.currentSampleTimeout != nil && p.currentThreadGroup != nil {
			lastSamplerIdx := len(p.currentThreadGroup.Samplers) - 1
			if lastSamplerIdx >= 0 {
				lastSampler := p.currentThreadGroup.Samplers[lastSamplerIdx]
				lastSampler.PreProcessors = append(lastSampler.PreProcessors, p.currentSampleTimeout)
			}
		}
		p.currentSampleTimeout = nil
	}
}

func (p *JmxParserV2) endElementProp() {
	if p.currentCookie != nil {
		if p.currentCookieManager != nil {
			p.currentCookieManager.Cookies = append(p.currentCookieManager.Cookies, *p.currentCookie)
		}
		p.currentCookie = nil
		return
	}
	if p.currentAuthorization != nil {
		if p.currentAuthManager != nil {
			p.currentAuthManager.AuthList = append(p.currentAuthManager.AuthList, *p.currentAuthorization)
		}
		p.currentAuthorization = nil
		return
	}
	if p.inMainControllerElementProp {
		p.inMainControllerElementProp = false
		return
	}
	if p.isArgumentProp {
		if p.currentReq == nil && p.currentArgName != "" {
			p.plan.UserDefinedVariables[p.currentArgName] = p.currentArgValue
		} else if p.inSystemSamplerArguments {
			if len(p.currentThreadGroup.Samplers) > 0 {
				lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				if lastS.IsOSProcessSampler {
					lastS.OSArguments = append(lastS.OSArguments, p.currentArgValue)
				}
			}
		} else if p.inSystemSamplerEnvironment && p.currentArgName != "" {
			if len(p.currentThreadGroup.Samplers) > 0 {
				lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
				if lastS.IsOSProcessSampler && lastS.OSEnvironment != nil {
					lastS.OSEnvironment[p.currentArgName] = p.currentArgValue
				}
			}
		} else if p.currentReq != nil {
			if p.postBodyRaw && p.currentReq.BodyTemplate == "" && p.currentArgName == "" {
				p.currentReq.BodyTemplate = p.currentArgValue
			} else if !p.postBodyRaw && p.currentArgName != "" {
				p.currentReq.Arguments = append(p.currentReq.Arguments, [2]string{p.currentArgName, p.currentArgValue})
			} else if p.postBodyRaw && p.currentArgName != "" {
				p.currentReq.Arguments = append(p.currentReq.Arguments, [2]string{p.currentArgName, p.currentArgValue})
			}
		}
		p.isArgumentProp = false
		p.currentArgName = ""
		p.currentArgValue = ""
		return
	}
	if p.inSystemSamplerArguments {
		p.inSystemSamplerArguments = false
		return
	}
	if p.inSystemSamplerEnvironment {
		p.inSystemSamplerEnvironment = false
		return
	}
}

func (p *JmxParserV2) endSampler(tag string) {
	switch {
	case tag == "AccessLogSampler" && p.currentReq != nil:
		pcol := p.protocolVal
		if pcol == "" {
			pcol = p.defProtocol
		}
		if pcol == "" {
			pcol = "http"
		}

		dom := p.domainVal
		if dom == "" {
			dom = p.defDomain
		}

		prt := p.portVal
		if prt == "" {
			prt = p.defPort
		}

		urlStr := pcol + "://" + dom
		if prt != "" {
			urlStr += ":" + prt
		}
		p.currentReq.URL = urlStr

		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			lastS.AccessLogDomain = dom
			lastS.AccessLogPort = prt
			lastS.AccessLogFile = p.currentLogFile
		}

		p.lastCompletedReq = p.currentReq
		p.currentReq = nil
		p.expectingSamplerChildTree = true

	case tag == "SystemSampler" && p.currentReq != nil:
		p.lastCompletedReq = p.currentReq
		p.currentReq = nil
		p.expectingSamplerChildTree = true

	case tag == "DebugSampler":
		p.currentDebugSampler = nil

	case tag == "TestAction":
		p.currentTestAction = nil

	case (tag == "HTTPSamplerProxy" || tag == "GraphQLHTTPSamplerProxy" || strings.Contains(tag, "WebSocketSampler") || strings.Contains(tag, "SSESampler")) && p.currentReq != nil:
		pcol := p.protocolVal
		if pcol == "" {
			pcol = p.defProtocol
		}
		if pcol == "" {
			pcol = "http"
		}

		dom := p.domainVal
		if dom == "" {
			dom = p.defDomain
		}

		prt := p.portVal
		if prt == "" {
			prt = p.defPort
		}

		pth := p.pathVal
		if pth == "" {
			pth = p.defPath
		}

		urlStr := pcol + "://" + dom
		if prt != "" {
			urlStr += ":" + prt
		}
		urlStr += pth
		p.currentReq.URL = urlStr

		if tag == "GraphQLHTTPSamplerProxy" || p.graphQLQuery != "" {
			bodyMap := map[string]interface{}{
				"query": p.graphQLQuery,
			}
			if p.graphQLVariables != "" {
				var varsMap map[string]interface{}
				if err := json.Unmarshal([]byte(p.graphQLVariables), &varsMap); err == nil {
					bodyMap["variables"] = varsMap
				} else {
					bodyMap["variables"] = p.graphQLVariables
				}
			}
			if p.graphQLOperationName != "" {
				bodyMap["operationName"] = p.graphQLOperationName
			}
			if b, err := json.Marshal(bodyMap); err == nil {
				p.currentReq.BodyTemplate = string(b)
			}
			p.graphQLQuery = ""
			p.graphQLVariables = ""
			p.graphQLOperationName = ""
		}

		p.lastCompletedReq = p.currentReq
		p.currentReq = nil
		p.expectingSamplerChildTree = true
	}
}

func (p *JmxParserV2) endUserParameters() {
	p.inUserParameters = false
	for i := 0; i < len(p.userParamNames) && i < len(p.userParamValues); i++ {
		p.plan.UserDefinedVariables[p.userParamNames[i]] = p.userParamValues[i]
	}
	p.userParamState = ""
}

func (p *JmxParserV2) endCollectionProp() {
	p.inDNSServers = false
	p.inDNSHosts = false
	p.userParamState = ""
	p.inModuleNodePath = false
	if p.inUltimateRow {
		p.inUltimateRow = false
		if p.currentThreadGroup != nil && p.currentThreadGroup.UltimateConfig != nil && len(p.ultimateRowVals) >= 5 {
			p.currentThreadGroup.UltimateConfig.Records = append(p.currentThreadGroup.UltimateConfig.Records, domain.UltimateScheduleRecord{
				StartThreads: p.ultimateRowVals[0],
				InitialDelay: p.ultimateRowVals[1],
				StartupTime:  p.ultimateRowVals[2],
				HoldLoadFor:  p.ultimateRowVals[3],
				ShutdownTime: p.ultimateRowVals[4],
			})
		}
	} else if p.inUltimateData {
		p.inUltimateData = false
	} else if p.inFreeFormRow {
		p.inFreeFormRow = false
		if p.currentThreadGroup != nil && p.currentThreadGroup.FreeFormArrivalsConfig != nil && len(p.freeFormRowVals) >= 3 {
			p.currentThreadGroup.FreeFormArrivalsConfig.Schedule = append(p.currentThreadGroup.FreeFormArrivalsConfig.Schedule, domain.FreeFormScheduleItem{
				Start:    p.freeFormRowVals[0],
				End:      p.freeFormRowVals[1],
				Duration: p.freeFormRowVals[2],
			})
		}
	} else if p.inFreeFormData {
		p.inFreeFormData = false
	}
}

func (p *JmxParserV2) handleCharData(cd xml.CharData) {
	val := strings.TrimSpace(string(cd))
	if val == "" {
		return
	}

	if p.inResultCollectorObjPropValue && p.currentResultCollector != nil {
		if p.currentResultCollector.Configuration == nil {
			p.currentResultCollector.Configuration = make(map[string]bool)
		}
		p.currentResultCollector.Configuration[p.currentTag] = (val == "true")
	}

	if p.inDNSServers {
		if p.currentDNSCacheManager != nil {
			p.currentDNSCacheManager.Servers = append(p.currentDNSCacheManager.Servers, val)
		}
	} else if p.inModuleNodePath && p.currentTag == "stringProp" {
		p.pendingModuleTargetNodePath = append(p.pendingModuleTargetNodePath, val)
	}

	switch p.currentTag {
	case "name":
		if p.floatPropNameState {
			p.floatPropName = val
		}
	case "value":
		p.handleValueCharData(val)
	case "boolProp":
		p.handleBoolPropCharData(val)
	case "intProp":
		p.handleIntPropCharData(val)
	case "longProp":
		p.handleLongPropCharData(val)
	case "stringProp":
		p.handleStringPropCharData(val)
	}
}

func (p *JmxParserV2) handleValueCharData(val string) {
	if p.floatPropValueState {
		switch p.floatPropName {
		case "ThroughputController.style":
			if v, err := strconv.Atoi(val); err == nil {
				p.pendingThroughputStyle = v
			}
		case "ThroughputController.percentThroughput":
			p.pendingThroughputMax = val
		case "throughput":
			if p.currentThroughputTimer != nil {
				p.currentThroughputTimer.Throughput = val
			}
		}
	}
}

func (p *JmxParserV2) handleBoolPropCharData(val string) {
	if p.nameAttr == "TestPlan.serialize_threadgroups" {
		p.plan.SerializeThreadGroups = (val == "true")
	}
	if p.nameAttr == "ThreadGroup.scheduler" && p.currentThreadGroup != nil {
		p.currentThreadGroup.Scheduler = (val == "true")
	}
	if p.currentDebugSampler != nil {
		switch p.nameAttr {
		case "displayJMeterVariables":
			p.currentDebugSampler.DebugJMeterVariables = (val == "true")
		case "displayJMeterProperties":
			p.currentDebugSampler.DebugJMeterProperties = (val == "true")
		case "displaySystemProperties":
			p.currentDebugSampler.DebugSystemProperties = (val == "true")
		}
	}
	if p.nameAttr == "SystemSampler.checkReturnCode" {
		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			if lastS.IsOSProcessSampler {
				lastS.OSCheckReturnCode = (val == "true")
			}
		}
	}
	if p.nameAttr == "ThroughputController.perThread" {
		p.pendingThroughputPerThread = (val == "true")
	}
	if p.nameAttr == "LoopController.continue_forever" {
		p.pendingLoopContinue = (val == "true")
		if p.currentThreadGroup != nil && p.inMainControllerElementProp {
			p.currentThreadGroup.ContinueForever = p.pendingLoopContinue
		}
	}
	if p.nameAttr == "ForeachController.useSeparator" {
		p.pendingForEachUseSeparator = (val == "true" || val == "")
	}
	if p.nameAttr == "HTTPSampler.postBodyRaw" && val == "true" {
		p.postBodyRaw = true
	}
	if p.currentReq != nil {
		switch p.nameAttr {
		case "HTTPSampler.follow_redirects", "HTTPSampler.auto_redirects":
			p.currentReq.FollowRedirects = (val == "true")
		}
	}
	if p.currentURLRewritingModifier != nil {
		switch p.nameAttr {
		case "path_extension":
			p.currentURLRewritingModifier.PathExtension = (val == "true")
		case "path_extension_no_equals":
			p.currentURLRewritingModifier.PathExtensionNoEq = (val == "true")
		case "path_extension_no_questionmark":
			p.currentURLRewritingModifier.PathExtensionNoQuestionMark = (val == "true")
		case "cache_value":
			p.currentURLRewritingModifier.ShouldCache = (val == "true")
		case "encode":
			p.currentURLRewritingModifier.Encode = (val == "true")
		}
	}
	if p.currentResultSaver != nil {
		switch p.nameAttr {
		case "FileSaver.errorsonly":
			p.currentResultSaver.ErrorsOnly = (val == "true")
		case "FileSaver.successonly":
			p.currentResultSaver.SuccessOnly = (val == "true")
		case "FileSaver.skipsuffix":
			p.currentResultSaver.SkipSuffix = (val == "true")
		case "FileSaver.skipautonumber":
			p.currentResultSaver.SkipAutoNumber = (val == "true")
		}
	}
	if p.currentCSVDataSet != nil {
		switch p.nameAttr {
		case "ignoreFirstLine":
			p.currentCSVDataSet.IgnoreFirstLine = (val == "true")
		case "quotedData":
			p.currentCSVDataSet.QuotedData = (val == "true")
		case "recycle":
			p.currentCSVDataSet.Recycle = (val == "true" || val == "")
		case "stopThread":
			p.currentCSVDataSet.StopThread = (val == "true")
		}
	}
	if p.currentCookieManager != nil {
		switch p.nameAttr {
		case "CookieManager.clearEachIteration":
			p.currentCookieManager.ClearEachIteration = (val == "true")
		case "CookieManager.controlledByThread":
			p.currentCookieManager.ControlledByThread = (val == "true")
		}
	}
	if p.currentJSONAssertion != nil {
		switch p.nameAttr {
		case "JSONVALIDATION":
			p.currentJSONAssertion.JSONValidation = (val == "true")
		case "EXPECT_NULL":
			p.currentJSONAssertion.ExpectNull = (val == "true")
		case "INVERT":
			p.currentJSONAssertion.Invert = (val == "true")
		case "ISREGEX":
			p.currentJSONAssertion.IsRegex = (val == "true")
		}
	}
	if p.currentXPathAssertion != nil {
		switch p.nameAttr {
		case "XPath.negate":
			p.currentXPathAssertion.Negate = (val == "true")
		case "XPath.validate":
			p.currentXPathAssertion.Validate = (val == "true")
		case "XPath.tolerant":
			p.currentXPathAssertion.Tolerant = (val == "true")
		case "XPath.whitespace":
			p.currentXPathAssertion.Whitespace = (val == "true")
		}
	}
	if p.currentCompareAssertion != nil {
		if p.nameAttr == "CompareAssertion.compareContent" {
			p.currentCompareAssertion.CompareContent = (val == "true")
		}
	}
	if p.currentSMIMEAssertion != nil {
		switch p.nameAttr {
		case "SMIMEAssertion.verifySignature":
			p.currentSMIMEAssertion.VerifySignature = (val == "true")
		case "SMIMEAssertion.notBefore":
			p.currentSMIMEAssertion.NotBefore = (val == "true")
		case "SMIMEAssertion.notAfter":
			p.currentSMIMEAssertion.NotAfter = (val == "true")
		}
	}
	if p.currentCookie != nil && p.nameAttr == "Cookie.secure" {
		p.currentCookie.Secure = (val == "true")
	}
	if p.currentCacheManager != nil {
		switch p.nameAttr {
		case "clearEachIteration":
			p.currentCacheManager.ClearEachIteration = (val == "true")
		case "useExpires":
			p.currentCacheManager.UseExpires = (val == "true")
		}
	}
	if p.currentCounter != nil && p.nameAttr == "CounterConfig.per_user" {
		p.currentCounter.PerUser = (val == "true")
	}
	if p.currentDNSCacheManager != nil {
		switch p.nameAttr {
		case "DNSCacheManager.clearEachIteration":
			p.currentDNSCacheManager.ClearEachIteration = (val == "true")
		case "DNSCacheManager.isCustomResolver":
			p.currentDNSCacheManager.IsCustomResolver = (val == "true")
		}
	}
	if p.currentAuthManager != nil && p.nameAttr == "AuthManager.clearEachIteration" {
		p.currentAuthManager.ClearEachIteration = (val == "true")
	}
	if p.currentRandomVariable != nil && p.nameAttr == "perThread" {
		p.currentRandomVariable.PerThread = (val == "true")
	}
	if p.currentResultCollector != nil && p.nameAttr == "ResultCollector.error_logging" {
		p.currentResultCollector.ErrorLogging = (val == "true")
	}
	if p.currentResultCollector != nil && p.nameAttr == "ResultCollector.success_only_logging" {
		p.currentResultCollector.SuccessOnlyLogging = (val == "true")
	}
	if p.currentBoundaryExtractor != nil && p.nameAttr == "BoundaryExtractor.default_empty_value" {
		p.currentBoundaryExtractor.DefaultEmptyValue = (val == "true")
	}
	if p.currentDebugPostProcessor != nil {
		switch p.nameAttr {
		case "displayJMeterVariables":
			p.currentDebugPostProcessor.DisplayJMeterVariables = (val == "true")
		case "displayJMeterProperties":
			p.currentDebugPostProcessor.DisplayJMeterProperties = (val == "true")
		case "displaySamplerProperties":
			p.currentDebugPostProcessor.DisplaySamplerProperties = (val == "true")
		case "displaySystemProperties":
			p.currentDebugPostProcessor.DisplaySystemProperties = (val == "true")
		}
	}
}

func (p *JmxParserV2) handleIntPropCharData(val string) {
	if p.currentTestAction != nil {
		switch p.nameAttr {
		case "ActionProcessor.action":
			if v, err := strconv.Atoi(val); err == nil {
				p.currentTestAction.TestActionAction = v
			}
		case "ActionProcessor.target":
			if v, err := strconv.Atoi(val); err == nil {
				p.currentTestAction.TestActionTarget = v
			}
		}
	}
	if p.nameAttr == "ThroughputController.style" {
		if v, err := strconv.Atoi(val); err == nil {
			p.pendingThroughputStyle = v
		}
	}
	if p.nameAttr == "ThreadGroup.num_threads" {
		if p.currentThreadGroup != nil {
			v, _ := strconv.Atoi(val)
			p.currentThreadGroup.NumThreads = v
			if p.currentThreadGroup.SteppingConfig != nil {
				p.currentThreadGroup.SteppingConfig.MaxRate = val
			}
		}
	}
	if p.nameAttr == "ThreadGroup.ramp_time" {
		if p.currentThreadGroup != nil {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentThreadGroup.RampUp = v
			}
		}
	}
	if p.currentCacheManager != nil && p.nameAttr == "maxSize" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentCacheManager.MaxSize = v
		}
	}
	if p.nameAttr == "LoopController.loops" {
		p.pendingLoopCountExpr = val
		if p.currentThreadGroup != nil && p.inMainControllerElementProp {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentThreadGroup.Loops = v
			}
		}
	}
	if p.currentResultAction != nil && p.nameAttr == "OnError.action" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentResultAction.Action = v
		}
	}
	if p.currentXPathExtractor != nil {
		switch p.nameAttr {
		case "XPathExtractor.fragment":
			p.currentXPathExtractor.Fragment = (val == "true")
		case "XPathExtractor.tolerant":
			p.currentXPathExtractor.Tolerant = (val == "true")
		case "XPathExtractor.namespace":
			p.currentXPathExtractor.NameSpace = (val == "true")
		}
	}
	if p.currentTimer != nil && p.currentTimer.Type == "SyncTimer" {
		if p.nameAttr == "groupSize" {
			p.currentTimer.GroupSize = val
		}
	}
	if p.currentResponseAssertion != nil && p.nameAttr == "Assertion.test_type" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentResponseAssertion.TestType = v
		}
	}
	if p.currentSizeAssertion != nil && p.nameAttr == "SizeAssertion.operator" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentSizeAssertion.Operator = v
		}
	}
	if p.currentCompareAssertion != nil && p.nameAttr == "CompareAssertion.compareTime" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentCompareAssertion.CompareTime = v
		}
	}
	if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
		switch p.nameAttr {
		case "MailerResultCollector.mailer_model.successLimit":
			if v, err := strconv.Atoi(val); err == nil {
				p.currentResultCollector.MailerModel.SuccessLimit = v
			}
		case "MailerResultCollector.mailer_model.failureLimit":
			if v, err := strconv.Atoi(val); err == nil {
				p.currentResultCollector.MailerModel.FailureLimit = v
			}
		}
	}
}

func (p *JmxParserV2) handleLongPropCharData(val string) {
	if p.currentTimer != nil && p.currentTimer.Type == "SyncTimer" {
		if p.nameAttr == "timeoutInMs" {
			p.currentTimer.TimeoutInMs = val
		}
	}
	if p.currentCompareAssertion != nil && p.nameAttr == "CompareAssertion.compareTime" {
		if v, err := strconv.Atoi(val); err == nil {
			p.currentCompareAssertion.CompareTime = v
		}
	}
}

func (p *JmxParserV2) handleStringPropCharData(val string) {
	if p.currentThreadGroup != nil {
		if p.nameAttr == "ThreadGroup.on_sample_error" {
			switch val {
			case "stopthread":
				p.currentThreadGroup.OnSampleError = 1
			case "stoptest":
				p.currentThreadGroup.OnSampleError = 2
			case "stoptestnow":
				p.currentThreadGroup.OnSampleError = 3
			case "startnextloop":
				p.currentThreadGroup.OnSampleError = 4
			default:
				p.currentThreadGroup.OnSampleError = 0
			}
		}
		if p.nameAttr == "ThreadGroup.duration" {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentThreadGroup.Duration = v
			}
		}
		if p.nameAttr == "ThreadGroup.delay" {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentThreadGroup.Delay = v
			}
		}
	}

	if p.currentTestAction != nil {
		if p.nameAttr == "ActionProcessor.duration" {
			p.currentTestAction.TestActionDuration = val
		}
	}

	if p.inUltimateRow {
		p.ultimateRowVals = append(p.ultimateRowVals, val)
		return
	}
	if p.inFreeFormRow {
		p.freeFormRowVals = append(p.freeFormRowVals, val)
		return
	}

	switch p.nameAttr {
	case "IfController.condition":
		p.pendingIfCondition = val
	case "TransactionController.parent":
		p.pendingTransactionParent = (val == "true")
	case "HTTPSampler.domain":
		if p.inConfigTestElement {
			p.defDomain = val
		} else {
			p.domainVal = val
		}
	case "HTTPSampler.port":
		if p.inConfigTestElement {
			p.defPort = val
		} else {
			p.portVal = val
		}
	case "HTTPSampler.path":
		if p.inConfigTestElement {
			p.defPath = val
		} else {
			p.pathVal = val
		}
	case "ConstantTimer.delay":
		if p.currentTimer != nil {
			p.currentTimer.Delay = val
		}
	case "RandomTimer.range":
		if p.currentTimer != nil {
			p.currentTimer.Range = val
		}
	case "timeoutInMs":
		if p.currentTimer != nil && p.currentTimer.Type == "SyncTimer" {
			p.currentTimer.TimeoutInMs = val
		}
	case "Assertion.custom_message":
		if p.currentResponseAssertion != nil {
			p.currentResponseAssertion.CustomFailure = val
		}
	case "Assertion.test_field":
		if p.currentResponseAssertion != nil {
			p.currentResponseAssertion.TestField = val
		}
		if p.currentSizeAssertion != nil {
			p.currentSizeAssertion.TestField = val
		}
	case "SizeAssertion.size":
		if p.currentSizeAssertion != nil {
			p.currentSizeAssertion.Size = val
		}
	case "XPath.xpath":
		if p.currentXPathAssertion != nil {
			p.currentXPathAssertion.XPath = val
		}
	case "DurationAssertion.duration":
		if p.currentDurationAssertion != nil {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentDurationAssertion.Duration = v
			}
		}
	case "MD5HexAssertion.size":
		if p.currentMD5HexAssertion != nil {
			p.currentMD5HexAssertion.ExpectedMD5Hex = val
		}
	case "SMIMEAssertion.signerDN":
		if p.currentSMIMEAssertion != nil {
			p.currentSMIMEAssertion.SignerDN = val
		}
	case "SMIMEAssertion.signerSerialNumber":
		if p.currentSMIMEAssertion != nil {
			p.currentSMIMEAssertion.SignerSerialNumber = val
		}
	case "SMIMEAssertion.signerEmail":
		if p.currentSMIMEAssertion != nil {
			p.currentSMIMEAssertion.SignerEmail = val
		}
	case "SMIMEAssertion.issuerDN":
		if p.currentSMIMEAssertion != nil {
			p.currentSMIMEAssertion.IssuerDN = val
		}
	case "JSON_PATH":
		if p.currentJSONAssertion != nil {
			p.currentJSONAssertion.JSONPath = val
		}
	case "EXPECTED_VALUE":
		if p.currentJSONAssertion != nil {
			p.currentJSONAssertion.ExpectedValue = val
		}
	case "throughput":
		if p.currentThroughputTimer != nil {
			p.currentThroughputTimer.Throughput = val
		}
	case "SSESampler.url":
		u, err := url.Parse(val)
		if err == nil {
			if u.Scheme == "https" || u.Scheme == "sses" {
				p.protocolVal = "sses"
			} else {
				p.protocolVal = "sse"
			}
			p.domainVal = u.Hostname()
			p.portVal = u.Port()
			p.pathVal = u.Path
			if u.RawQuery != "" {
				p.pathVal += "?" + u.RawQuery
			}
		}
	case "HTTPSampler.protocol":
		if p.inConfigTestElement {
			p.defProtocol = val
		} else {
			p.protocolVal = val
		}
	case "SystemSampler.command":
		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			if lastS.IsOSProcessSampler {
				lastS.OSCommand = val
			}
		}
	case "SystemSampler.directory":
		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			if lastS.IsOSProcessSampler {
				lastS.OSDirectory = val
			}
		}
	case "SystemSampler.timeout":
		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			if lastS.IsOSProcessSampler {
				lastS.OSTimeout = val
			}
		}
	case "SystemSampler.expectedReturnCode":
		if len(p.currentThreadGroup.Samplers) > 0 {
			lastS := p.currentThreadGroup.Samplers[len(p.currentThreadGroup.Samplers)-1]
			if lastS.IsOSProcessSampler {
				lastS.OSExpectedReturnCode = val
			}
		}
	case "HTTPSampler.method", "method":
		if p.currentReq != nil {
			p.currentReq.Method = val
		}
	case "server", "serverAddress", "domain":
		p.domainVal = val
	case "port", "serverPort", "portString":
		p.portVal = val
	case "path", "contextPath":
		p.pathVal = val
	case "logFile":
		p.currentLogFile = val
	case "requestPayload":
		if p.currentReq != nil {
			p.currentReq.BodyTemplate = val
		}
	case "GraphQLHTTPSampler.query":
		p.graphQLQuery = val
	case "GraphQLHTTPSampler.variables":
		p.graphQLVariables = val
	case "GraphQLHTTPSampler.operationName":
		p.graphQLOperationName = val
	case "TLS", "protocol":
		p.protocolVal = val
	case "ThreadGroup.num_threads":
		if p.currentThreadGroup != nil {
			v, _ := strconv.Atoi(val)
			p.currentThreadGroup.NumThreads = v
			if p.currentThreadGroup.SteppingConfig != nil {
				p.currentThreadGroup.SteppingConfig.MaxRate = val
			}
		}
	case "OpenModelThreadGroup.schedule":
		if p.currentThreadGroup != nil {
			p.currentThreadGroup.OpenModelSchedule = val
		}
	case "Threads initial delay":
		if p.currentThreadGroup != nil && p.currentThreadGroup.SteppingConfig != nil {
			p.currentThreadGroup.SteppingConfig.InitialDelay = val
		}
	case "Start users count":
		if p.currentThreadGroup != nil && p.currentThreadGroup.SteppingConfig != nil {
			p.currentThreadGroup.SteppingConfig.StepRate = val
		}
	case "Start users period":
		if p.currentThreadGroup != nil && p.currentThreadGroup.SteppingConfig != nil {
			p.currentThreadGroup.SteppingConfig.StepDuration = val
		}
	case "flighttime":
		if p.currentThreadGroup != nil && p.currentThreadGroup.SteppingConfig != nil {
			p.currentThreadGroup.SteppingConfig.HoldDuration = val
		}
	case "TargetLevel":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ConcurrencyConfig != nil {
			p.currentThreadGroup.ConcurrencyConfig.TargetLevel = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.TargetLevel = val
		}
	case "RampUp":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ConcurrencyConfig != nil {
			p.currentThreadGroup.ConcurrencyConfig.RampUp = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.RampUp = val
		}
	case "Steps":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ConcurrencyConfig != nil {
			p.currentThreadGroup.ConcurrencyConfig.Steps = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.Steps = val
		}
	case "Hold":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ConcurrencyConfig != nil {
			p.currentThreadGroup.ConcurrencyConfig.Hold = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.Hold = val
		}
	case "Unit":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ConcurrencyConfig != nil {
			p.currentThreadGroup.ConcurrencyConfig.Unit = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.Unit = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.FreeFormArrivalsConfig != nil {
			p.currentThreadGroup.FreeFormArrivalsConfig.Unit = val
		}
	case "ConcurrencyLimit":
		if p.currentThreadGroup != nil && p.currentThreadGroup.ArrivalsConfig != nil {
			p.currentThreadGroup.ArrivalsConfig.ConcurrencyLimit = val
		} else if p.currentThreadGroup != nil && p.currentThreadGroup.FreeFormArrivalsConfig != nil {
			p.currentThreadGroup.FreeFormArrivalsConfig.ConcurrencyLimit = val
		}
	case "ThroughputController.maxThroughput":
		p.pendingThroughputMax = val
	case "SwitchController.value":
		p.pendingSwitchValue = val
	case "IncludeController.includepath":
		p.pendingIncludePath = val
	case "Argument.name":
		p.currentArgName = val
	case "LoopController.loops":
		p.pendingLoopCountExpr = val
		if p.currentThreadGroup != nil && p.inMainControllerElementProp {
			if v, err := strconv.Atoi(val); err == nil {
				p.currentThreadGroup.Loops = v
			} else if val == "-1" {
				p.currentThreadGroup.Loops = -1
			}
		}
	case "WhileController.condition":
		p.pendingWhileCondition = val
	case "CriticalSectionController.lockName":
		p.pendingCriticalLockName = val
	case "ForeachController.inputVal":
		p.pendingForEachInputVal = val
	case "ForeachController.returnVal":
		p.pendingForEachReturnVal = val
	case "ForeachController.startIndex":
		p.pendingForEachStartIndex = val
	case "ForeachController.endIndex":
		p.pendingForEachEndIndex = val
	case "RunTime.seconds":
		p.pendingRuntimeSeconds = val
	case "Argument.value":
		p.currentArgValue = val
	case "Header.name":
		if p.inHeaderManager {
			p.currentHeaderName = val
		}
	case "Header.value":
		if p.inHeaderManager && p.currentHeaderName != "" {
			target := p.currentReq
			if target == nil {
				target = p.lastCompletedReq
			}
			if target != nil {
				target.Headers[p.currentHeaderName] = val
			}
			p.currentHeaderName = ""
		}
	case "JSONPostProcessor.referenceNames":
		if p.inJSONExtractor && p.currentJSONExtractor != nil {
			p.currentJSONExtractor.ReferenceName = val
		}
	case "JSONPostProcessor.jsonPathExprs":
		if p.inJSONExtractor && p.currentJSONExtractor != nil {
			p.currentJSONExtractor.JSONPathExpr = val
		}
	case "JSONPostProcessor.defaultValues":
		if p.inJSONExtractor && p.currentJSONExtractor != nil {
			p.currentJSONExtractor.DefaultValueStr = val
		}
	case "JSONPostProcessor.match_numbers":
		if p.inJSONExtractor && p.currentJSONExtractor != nil {
			if num, err := strconv.Atoi(val); err == nil {
				p.currentJSONExtractor.MatchNo = num
			}
		}
	case "HtmlExtractor.refname":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			p.currentHtmlExtractor.ReferenceName = val
		}
	case "HtmlExtractor.expr":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			p.currentHtmlExtractor.Expr = val
		}
	case "HtmlExtractor.attribute":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			p.currentHtmlExtractor.Attribute = val
		}
	case "HtmlExtractor.match_number":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			if m, err := strconv.Atoi(val); err == nil {
				p.currentHtmlExtractor.MatchNo = m
			}
		}
	case "HtmlExtractor.default":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			p.currentHtmlExtractor.DefaultValueStr = val
		}
	case "JMESPathExtractor.referenceName":
		if p.inJMESPathExtractor && p.currentJMESPathExtractor != nil {
			p.currentJMESPathExtractor.ReferenceName = val
		}
	case "JMESPathExtractor.jmesPathExpr":
		if p.inJMESPathExtractor && p.currentJMESPathExtractor != nil {
			p.currentJMESPathExtractor.JmesPathExpr = val
		}
	case "JMESPathExtractor.matchNumber":
		if p.inJMESPathExtractor && p.currentJMESPathExtractor != nil {
			if m, err := strconv.Atoi(val); err == nil {
				p.currentJMESPathExtractor.MatchNo = m
			}
		}
	case "JMESPathExtractor.defaultValue":
		if p.inJMESPathExtractor && p.currentJMESPathExtractor != nil {
			p.currentJMESPathExtractor.DefaultValueStr = val
		}
	case "BoundaryExtractor.refname":
		if p.inBoundaryExtractor && p.currentBoundaryExtractor != nil {
			p.currentBoundaryExtractor.ReferenceName = val
		}
	case "BoundaryExtractor.lboundary":
		if p.inBoundaryExtractor && p.currentBoundaryExtractor != nil {
			p.currentBoundaryExtractor.LBoundary = val
		}
	case "BoundaryExtractor.rboundary":
		if p.inBoundaryExtractor && p.currentBoundaryExtractor != nil {
			p.currentBoundaryExtractor.RBoundary = val
		}
	case "BoundaryExtractor.match_number":
		if p.inBoundaryExtractor && p.currentBoundaryExtractor != nil {
			if m, err := strconv.Atoi(val); err == nil {
				p.currentBoundaryExtractor.MatchNo = m
			}
		}
	case "BoundaryExtractor.default":
		if p.inBoundaryExtractor && p.currentBoundaryExtractor != nil {
			p.currentBoundaryExtractor.DefaultValueStr = val
		}
	case "XPathExtractor.refname":
		if p.inXPathExtractor && p.currentXPathExtractor != nil {
			p.currentXPathExtractor.ReferenceName = val
		}
	case "XPathExtractor.xpathQuery":
		if p.inXPathExtractor && p.currentXPathExtractor != nil {
			p.currentXPathExtractor.XPathQuery = val
		}
	case "XPathExtractor.default":
		if p.inXPathExtractor && p.currentXPathExtractor != nil {
			p.currentXPathExtractor.DefaultVal = val
		}
	case "XPathExtractor.matchNumber":
		if p.inXPathExtractor && p.currentXPathExtractor != nil {
			if m, err := strconv.Atoi(val); err == nil {
				p.currentXPathExtractor.MatchNumber = m
			}
		}
	case "RegexExtractor.refname":
		if p.inRegexExtractor && p.currentRegexExtractor != nil {
			p.currentRegexExtractor.ReferenceName = val
		}
	case "RegexExtractor.regex":
		if p.inRegexExtractor && p.currentRegexExtractor != nil {
			p.currentRegexExtractor.Regex = val
		}
	case "RegexExtractor.template":
		if p.inRegexExtractor && p.currentRegexExtractor != nil {
			p.currentRegexExtractor.Template = val
		}
	case "RegexExtractor.default":
		if p.inRegexExtractor && p.currentRegexExtractor != nil {
			p.currentRegexExtractor.DefaultValueStr = val
		}
	case "RegexExtractor.match_number":
		if p.inRegexExtractor && p.currentRegexExtractor != nil {
			if num, err := strconv.Atoi(val); err == nil {
				p.currentRegexExtractor.MatchNo = num
			}
		}
	case "HtmlExtractor.default_empty_value":
		if p.inHtmlExtractor && p.currentHtmlExtractor != nil {
			p.currentHtmlExtractor.DefaultEmptyValue = (val == "true")
		}
	case "filename":
		if p.currentCSVDataSet != nil {
			p.currentCSVDataSet.Filename = val
		}
		if p.currentResultCollector != nil {
			p.currentResultCollector.Filename = val
		}
	case "fileEncoding":
		if p.currentCSVDataSet != nil {
			p.currentCSVDataSet.FileEncoding = val
		}
	case "variableNames":
		if p.currentCSVDataSet != nil {
			p.currentCSVDataSet.VariableNames = val
		}
	case "delimiter":
		if p.currentCSVDataSet != nil {
			p.currentCSVDataSet.Delimiter = val
		}
	case "shareMode":
		if p.currentCSVDataSet != nil {
			p.currentCSVDataSet.ShareMode = val
		}
	case "Cookie.value":
		if p.currentCookie != nil {
			p.currentCookie.Value = val
		}
	case "Cookie.domain":
		if p.currentCookie != nil {
			p.currentCookie.Domain = val
		}
	case "Cookie.path":
		if p.currentCookie != nil {
			p.currentCookie.Path = val
		}
	case "CounterConfig.start":
		if p.currentCounter != nil {
			p.currentCounter.Start = val
		}
	case "CounterConfig.end":
		if p.currentCounter != nil {
			p.currentCounter.End = val
		}
	case "CounterConfig.incr":
		if p.currentCounter != nil {
			p.currentCounter.Incr = val
		}
	case "CounterConfig.name":
		if p.currentCounter != nil {
			p.currentCounter.Name = val
		}
	case "CounterConfig.format":
		if p.currentCounter != nil {
			p.currentCounter.Format = val
		}
	case "StaticHost.Name":
		if p.currentDNSCacheManager != nil && p.inDNSHosts {
			p.currentStaticHostName = val
		}
	case "StaticHost.Address":
		if p.currentDNSCacheManager != nil && p.inDNSHosts && p.currentStaticHostName != "" {
			p.currentDNSCacheManager.Hosts[p.currentStaticHostName] = val
			p.currentStaticHostName = ""
		}
	case "Authorization.url":
		if p.currentAuthorization != nil {
			p.currentAuthorization.URL = val
		}
	case "Authorization.username":
		if p.currentAuthorization != nil {
			p.currentAuthorization.Username = val
		}
	case "Authorization.password":
		if p.currentAuthorization != nil {
			p.currentAuthorization.Password = val
		}
	case "Authorization.mechanism":
		if p.currentAuthorization != nil {
			p.currentAuthorization.Mechanism = val
		}
	case "maximumValue":
		if p.currentRandomVariable != nil {
			p.currentRandomVariable.MaximumValue = val
		}
	case "minimumValue":
		if p.currentRandomVariable != nil {
			p.currentRandomVariable.MinimumValue = val
		}
	case "outputFormat":
		if p.currentRandomVariable != nil {
			p.currentRandomVariable.Format = val
		}
	case "randomSeed":
		if p.currentRandomVariable != nil {
			p.currentRandomVariable.RandomSeed = val
		}
	case "variableName":
		if p.currentRandomVariable != nil {
			p.currentRandomVariable.Name = val
		}
	case "argument_name":
		if p.inURLRewritingModifier && p.currentURLRewritingModifier != nil {
			p.currentURLRewritingModifier.ArgumentName = val
		}
	case "RegExUserParameters.regex_ref_name":
		if p.inRegExUserParameters && p.currentRegExUserParameters != nil {
			p.currentRegExUserParameters.RegexRefName = val
		}
	case "RegExUserParameters.param_names_gr_nr":
		if p.inRegExUserParameters && p.currentRegExUserParameters != nil {
			p.currentRegExUserParameters.ParamNamesGrNr = val
		}
	case "RegExUserParameters.param_values_gr_nr":
		if p.inRegExUserParameters && p.currentRegExUserParameters != nil {
			p.currentRegExUserParameters.ParamValuesGrNr = val
		}
	case "SampleTimeout.timeout":
		if p.inSampleTimeout && p.currentSampleTimeout != nil {
			p.currentSampleTimeout.Timeout = val
		}
	case "classname":
		if p.currentBackendListener != nil {
			p.currentBackendListener.Classname = val
		}
	case "FileSaver.filename":
		if p.currentResultSaver != nil {
			p.currentResultSaver.FilenamePrefix = val
		}
	case "MailerResultCollector.mailer_model.failureSubject":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.FailureSubject = val
		}
	case "MailerResultCollector.mailer_model.successSubject":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.SuccessSubject = val
		}
	case "MailerResultCollector.mailer_model.fromAddress":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.FromAddress = val
		}
	case "MailerResultCollector.mailer_model.addressie":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.ToAddress = val
		}
	case "MailerResultCollector.mailer_model.smtpHost":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.SmtpHost = val
		}
	case "MailerResultCollector.mailer_model.smtpPort":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.SmtpPort = val
		}
	case "MailerResultCollector.mailer_model.login":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.Username = val
		}
	case "MailerResultCollector.mailer_model.password":
		if p.currentResultCollector != nil && p.currentResultCollector.MailerModel != nil {
			p.currentResultCollector.MailerModel.Password = val
		}
	default:
		if p.inResponseAssertion && p.currentResponseAssertion != nil {
			if !strings.HasPrefix(p.nameAttr, "Assertion.") {
				p.currentResponseAssertion.TestStrings = append(p.currentResponseAssertion.TestStrings, val)
			}
		}
		if p.inDNSServers && p.currentDNSCacheManager != nil {
			p.currentDNSCacheManager.Servers = append(p.currentDNSCacheManager.Servers, val)
		}
		if p.inUserParameters {
			switch p.userParamState {
			case "names":
				p.userParamNames = append(p.userParamNames, val)
			case "values":
				p.userParamValues = append(p.userParamValues, val)
			}
		}
	}
}

func (p *JmxParserV2) postProcessControllers() {
	for _, tg := range p.plan.ThreadGroups {
		for i, s := range tg.Samplers {
			switch s.ControlType {
			case "InterleaveStart":
				childStarts := []int{}
				childEnds := []int{}
				for j := i + 1; j < s.BlockEndIndex; {
					childStarts = append(childStarts, j)

					endIdx := j
					if tg.Samplers[j].IsControlFlow && tg.Samplers[j].BlockEndIndex > 0 {
						endIdx = tg.Samplers[j].BlockEndIndex
					}
					childEnds = append(childEnds, endIdx)
					j = endIdx + 1
				}
				s.InterleaveChildStarts = childStarts
				s.InterleaveChildEnds = childEnds

			case "RandomStart":
				childStarts := []int{}
				childEnds := []int{}
				for j := i + 1; j < s.BlockEndIndex; {
					childStarts = append(childStarts, j)

					endIdx := j
					if tg.Samplers[j].IsControlFlow && tg.Samplers[j].BlockEndIndex > 0 {
						endIdx = tg.Samplers[j].BlockEndIndex
					}
					childEnds = append(childEnds, endIdx)
					j = endIdx + 1
				}
				s.RandomChildStarts = childStarts
				s.RandomChildEnds = childEnds

			case "RandomOrderStart":
				childStarts := []int{}
				childEnds := []int{}
				for j := i + 1; j < s.BlockEndIndex; {
					childStarts = append(childStarts, j)

					endIdx := j
					if tg.Samplers[j].IsControlFlow && tg.Samplers[j].BlockEndIndex > 0 {
						endIdx = tg.Samplers[j].BlockEndIndex
					}
					childEnds = append(childEnds, endIdx)
					j = endIdx + 1
				}
				s.RandomOrderChildStarts = childStarts
				s.RandomOrderChildEnds = childEnds

			case "SwitchStart":
				childStarts := []int{}
				childEnds := []int{}
				childNames := []string{}
				for j := i + 1; j < s.BlockEndIndex; {
					childStarts = append(childStarts, j)
					childNames = append(childNames, tg.Samplers[j].Name)

					endIdx := j
					if tg.Samplers[j].IsControlFlow && tg.Samplers[j].BlockEndIndex > 0 {
						endIdx = tg.Samplers[j].BlockEndIndex
					}
					childEnds = append(childEnds, endIdx)
					j = endIdx + 1
				}
				s.SwitchChildStarts = childStarts
				s.SwitchChildEnds = childEnds
				s.SwitchChildNames = childNames
			}
		}
	}
}
