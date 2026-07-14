package domain

type Assertion interface {
	IsAssertion() bool
}

type ResponseAssertion struct {
	Name          string
	TestField     string   // "Assertion.test_field"
	TestType      int      // "Assertion.test_type"
	TestStrings   []string // "collectionProp name=Asserion.test_strings"
	CustomFailure string   // "Assertion.custom_message"
}

func (*ResponseAssertion) IsAssertion() bool { return true }

type JSONAssertion struct {
	Name           string
	JSONPath       string // "JSON_PATH"
	ExpectedValue  string // "EXPECTED_VALUE"
	JSONValidation bool   // "JSONVALIDATION"
	ExpectNull     bool   // "EXPECT_NULL"
	Invert         bool   // "INVERT"
	IsRegex        bool   // "ISREGEX"
}

func (*JSONAssertion) IsAssertion() bool { return true }

type SizeAssertion struct {
	Name      string
	TestField string // "Assertion.test_field"
	Size      string // "SizeAssertion.size"
	Operator  int    // "SizeAssertion.operator"
}

func (*SizeAssertion) IsAssertion() bool { return true }
