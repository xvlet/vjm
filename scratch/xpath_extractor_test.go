package scratch

import (
	"context"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta/engine"
)

func TestXPathExtractorParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_xpath_extractor.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("Expected ThreadGroup, got 0")
	}

	tg := plan.ThreadGroups[0]
	if len(tg.Samplers) == 0 {
		t.Fatalf("Expected samplers, got 0")
	}

	sampler := tg.Samplers[0]
	var extAll, extNth *domain.XPathExtractor
	for _, ext := range sampler.Extractors {
		if x, ok := ext.(*domain.XPathExtractor); ok {
			if x.ReferenceName == "titleAll" {
				extAll = x
			} else if x.ReferenceName == "titleNth" {
				extNth = x
			}
		}
	}

	if extAll == nil || extNth == nil {
		t.Fatalf("Failed to parse XPath Extractors")
	}

	if extAll.XPathQuery != "//title" {
		t.Errorf("Expected //title, got %s", extAll.XPathQuery)
	}
	if extAll.MatchNumber != -1 {
		t.Errorf("Expected -1, got %d", extAll.MatchNumber)
	}

	if extNth.MatchNumber != 2 {
		t.Errorf("Expected 2, got %d", extNth.MatchNumber)
	}
}

func TestXPathExtractorExecution(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_xpath_extractor.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	eval := evaluator.NewDefaultEvaluator(nil)
	attacker := engine.NewStatefulAttacker(1, vegeta.ConstantPacer{Freq: 1, Per: time.Second}, 3*time.Second)

	results := attacker.Attack(context.Background(), plan, eval)

	// We only run 1 attack, we can't easily intercept the session variables from Attack results directly
	// unless we test it manually by injecting DebugPostProcessor or similar.
	// But we can test ExtractMulti directly!

	sampler := plan.ThreadGroups[0].Samplers[0]
	var extAll, extNth *domain.XPathExtractor
	for _, ext := range sampler.Extractors {
		if x, ok := ext.(*domain.XPathExtractor); ok {
			if x.ReferenceName == "titleAll" {
				extAll = x
			} else if x.ReferenceName == "titleNth" {
				extNth = x
			}
		}
	}

	body := []byte(`
		<library>
		  <book>
			<title>Sayings of the Century</title>
			<price>8.95</price>
		  </book>
		  <book>
			<title>Moby Dick</title>
			<price>8.99</price>
		  </book>
		</library>
	`)

	resAll, okAll := extAll.ExtractMulti(body)
	if !okAll {
		t.Fatalf("Expected extAll to succeed")
	}

	if resAll["titleAll_matchNr"] != "2" {
		t.Errorf("Expected titleAll_matchNr=2, got %s", resAll["titleAll_matchNr"])
	}
	if resAll["titleAll_1"] != "Sayings of the Century" {
		t.Errorf("Expected titleAll_1='Sayings of the Century', got %s", resAll["titleAll_1"])
	}
	if resAll["titleAll_2"] != "Moby Dick" {
		t.Errorf("Expected titleAll_2='Moby Dick', got %s", resAll["titleAll_2"])
	}

	resNth, okNth := extNth.ExtractMulti(body)
	if !okNth {
		t.Fatalf("Expected extNth to succeed")
	}
	if resNth["titleNth"] != "Moby Dick" {
		t.Errorf("Expected titleNth='Moby Dick', got %s", resNth["titleNth"])
	}

	// Just consume the channel
	for range results {
	}
}
