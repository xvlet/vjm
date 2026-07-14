package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestMailerVisualizerParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_mailer_visualizer.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ResultCollectors) == 0 {
		t.Fatalf("Expected at least 1 ResultCollector (MailerResultCollector) at plan level")
	}

	found := false
	for _, rc := range plan.ResultCollectors {
		if rc.Filename == "mailer_results.jtl" {
			found = true
			if rc.ErrorLogging {
				t.Errorf("Expected ErrorLogging false")
			}
			if rc.MailerModel == nil {
				t.Fatalf("Expected MailerModel to be parsed")
			}
			if rc.MailerModel.SuccessLimit != 2 {
				t.Errorf("Expected SuccessLimit 2, got %d", rc.MailerModel.SuccessLimit)
			}
			if rc.MailerModel.FailureLimit != 2 {
				t.Errorf("Expected FailureLimit 2, got %d", rc.MailerModel.FailureLimit)
			}
			if rc.MailerModel.FailureSubject != "Test Failed" {
				t.Errorf("Expected FailureSubject 'Test Failed', got %s", rc.MailerModel.FailureSubject)
			}
			if rc.MailerModel.SmtpHost != "smtp.test.com" {
				t.Errorf("Expected SmtpHost 'smtp.test.com', got %s", rc.MailerModel.SmtpHost)
			}
			if rc.MailerModel.ToAddress != "receiver@test.com" {
				t.Errorf("Expected ToAddress 'receiver@test.com', got %s", rc.MailerModel.ToAddress)
			}
			if rc.MailerModel.SmtpPort != "587" {
				t.Errorf("Expected SmtpPort '587', got %s", rc.MailerModel.SmtpPort)
			}
			if rc.MailerModel.Username != "user" {
				t.Errorf("Expected Username 'user', got %s", rc.MailerModel.Username)
			}
			if rc.MailerModel.Password != "pass123" {
				t.Errorf("Expected Password 'pass123', got %s", rc.MailerModel.Password)
			}
		}
	}

	if !found {
		t.Fatalf("Mailer Visualizer listener not found in plan.ResultCollectors")
	}
}
