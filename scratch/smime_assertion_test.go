package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestSMIMEAssertionParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/assertions/test_smime_assertion.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) < 1 {
		t.Fatalf("Expected at least 1 sampler, got %d", len(tg.Samplers))
	}

	// Req1
	req1 := tg.Samplers[0]
	if len(req1.Assertions) == 0 {
		t.Fatalf("Req1 missing assertions")
	}
	sa, ok := req1.Assertions[0].(*domain.SMIMEAssertion)
	if !ok {
		t.Fatalf("Req1 assertion is not SMIMEAssertion")
	}

	if !sa.VerifySignature {
		t.Errorf("Expected VerifySignature to be true")
	}
	if !sa.NotBefore {
		t.Errorf("Expected NotBefore to be true")
	}
	if !sa.NotAfter {
		t.Errorf("Expected NotAfter to be true")
	}
	if sa.SignerDN != "CN=Test Signer,O=Test Org" {
		t.Errorf("Unexpected SignerDN: %s", sa.SignerDN)
	}
	if sa.SignerSerialNumber != "123456" {
		t.Errorf("Unexpected SignerSerialNumber: %s", sa.SignerSerialNumber)
	}
	if sa.SignerEmail != "test@example.com" {
		t.Errorf("Unexpected SignerEmail: %s", sa.SignerEmail)
	}
	if sa.IssuerDN != "CN=Test Issuer,O=Test Org" {
		t.Errorf("Unexpected IssuerDN: %s", sa.IssuerDN)
	}
}
