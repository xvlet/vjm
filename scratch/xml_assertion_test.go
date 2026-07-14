package scratch

import (
	"bytes"
	"encoding/xml"
	"io"
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestXMLAssertionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/assertions/test_xml_assertion.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) < 2 {
		t.Fatalf("Expected at least 2 samplers, got %d", len(tg.Samplers))
	}

	// Req1
	req1 := tg.Samplers[0]
	if len(req1.Assertions) == 0 {
		t.Fatalf("Req1 missing assertions")
	}
	_, ok := req1.Assertions[0].(*domain.XMLAssertion)
	if !ok {
		t.Fatalf("Req1 assertion is not XMLAssertion")
	}
}

func TestXMLAssertionLogic(t *testing.T) {
	validXML := []byte(`<root><hello>world</hello></root>`)
	invalidXML := []byte(`<root><hello>world</hello>`) // missing </root>

	err1 := checkXML(validXML)
	if err1 != nil {
		t.Errorf("Expected validXML to pass, got: %v", err1)
	}

	err2 := checkXML(invalidXML)
	if err2 == nil {
		t.Errorf("Expected invalidXML to fail")
	}
}

func checkXML(bodyBytes []byte) error {
	if err := xml.Unmarshal(bodyBytes, new(interface{})); err != nil {
		decoder := xml.NewDecoder(bytes.NewReader(bodyBytes))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}
