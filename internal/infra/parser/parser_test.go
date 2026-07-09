package parser

import (
	"os"
	"testing"
)

func TestParseJMXBasic(t *testing.T) {
	jmxContent := `<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0" jmeter="5.5">
  <hashTree>
    <TestPlan guiclass="TestPlanGui" testclass="TestPlan" testname="Basic Plan" enabled="true">
      <stringProp name="TestPlan.comments"></stringProp>
      <boolProp name="TestPlan.functional_mode">false</boolProp>
      <boolProp name="TestPlan.tearDown_on_shutdown">true</boolProp>
      <boolProp name="TestPlan.serialize_threadgroups">false</boolProp>
      <elementProp name="TestPlan.user_defined_variables" elementType="Arguments" guiclass="ArgumentsPanel" testclass="Arguments" testname="User Defined Variables" enabled="true">
        <collectionProp name="Arguments.arguments">
          <elementProp name="HOST" elementType="Argument">
            <stringProp name="Argument.name">HOST</stringProp>
            <stringProp name="Argument.value">localhost</stringProp>
            <stringProp name="Argument.metadata">=</stringProp>
          </elementProp>
        </collectionProp>
      </elementProp>
      <stringProp name="TestPlan.user_define_classpath"></stringProp>
    </TestPlan>
    <hashTree>
      <ThreadGroup guiclass="ThreadGroupGui" testclass="ThreadGroup" testname="Thread Group 1" enabled="true">
        <stringProp name="ThreadGroup.num_threads">10</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <boolProp name="ThreadGroup.same_user_on_next_iteration">true</boolProp>
        <stringProp name="ThreadGroup.on_sample_error">continue</stringProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController" guiclass="LoopControlPanel" testclass="LoopController" testname="Loop Controller" enabled="true">
          <boolProp name="LoopController.continue_forever">false</boolProp>
          <stringProp name="LoopController.loops">1</stringProp>
        </elementProp>
      </ThreadGroup>
      <hashTree>
        <HTTPSamplerProxy guiclass="HttpTestSampleGui" testclass="HTTPSamplerProxy" testname="HTTP Request 1" enabled="true">
          <elementProp name="HTTPsampler.Arguments" elementType="Arguments" guiclass="HTTPArgumentsPanel" testclass="Arguments" testname="User Defined Variables" enabled="true">
            <collectionProp name="Arguments.arguments"/>
          </elementProp>
          <stringProp name="HTTPSampler.domain">${HOST}</stringProp>
          <stringProp name="HTTPSampler.port">8080</stringProp>
          <stringProp name="HTTPSampler.protocol">http</stringProp>
          <stringProp name="HTTPSampler.contentEncoding"></stringProp>
          <stringProp name="HTTPSampler.path">/api/v1/test</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
          <boolProp name="HTTPSampler.follow_redirects">true</boolProp>
          <boolProp name="HTTPSampler.auto_redirects">false</boolProp>
          <boolProp name="HTTPSampler.use_keepalive">true</boolProp>
          <boolProp name="HTTPSampler.DO_MULTIPART_POST">false</boolProp>
          <stringProp name="HTTPSampler.embedded_url_re"></stringProp>
          <stringProp name="HTTPSampler.connect_timeout"></stringProp>
          <stringProp name="HTTPSampler.response_timeout"></stringProp>
        </HTTPSamplerProxy>
        <hashTree/>
      </hashTree>
    </hashTree>
  </hashTree>
</jmeterTestPlan>`

	tmpFile, err := os.CreateTemp("", "testplan_*.jmx")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, err = tmpFile.WriteString(jmxContent)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	parser := NewDefaultJmxParser()
	plan, err := parser.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if plan.Name != "Basic Plan" {
		t.Errorf("expected plan name 'Basic Plan', got '%s'", plan.Name)
	}

	if plan.UserDefinedVariables["HOST"] != "localhost" {
		t.Errorf("expected HOST=localhost, got '%s'", plan.UserDefinedVariables["HOST"])
	}

	if len(plan.ThreadGroups) != 1 {
		t.Fatalf("expected 1 ThreadGroup, got %d", len(plan.ThreadGroups))
	}

	tg := plan.ThreadGroups[0]
	if tg.Name != "Thread Group 1" {
		t.Errorf("expected Thread Group 1, got '%s'", tg.Name)
	}

	if len(tg.Samplers) != 1 {
		t.Fatalf("expected 1 Sampler, got %d", len(tg.Samplers))
	}

	sampler := tg.Samplers[0]
	if sampler.Name != "HTTP Request 1" {
		t.Errorf("expected 'HTTP Request 1', got '%s'", sampler.Name)
	}

	if sampler.Request.Method != "GET" {
		t.Errorf("expected GET, got '%s'", sampler.Request.Method)
	}

	if sampler.Request.URL != "http://${HOST}:8080/api/v1/test" {
		t.Errorf("unexpected URL: %s", sampler.Request.URL)
	}
}
