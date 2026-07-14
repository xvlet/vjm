package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestCssSelectorExtractorParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_css_selector_extractor.jmx")
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

	req1 := tg.Samplers[0]
	if len(req1.Extractors) == 0 {
		t.Fatalf("Req1 missing Extractors")
	}

	ext, ok := req1.Extractors[0].(*domain.HtmlExtractor)
	if !ok {
		t.Fatalf("Req1 Extractor is not HtmlExtractor")
	}

	if ext.ReferenceName != "EXTRACTED_DATA" {
		t.Errorf("Expected ReferenceName 'EXTRACTED_DATA', got '%s'", ext.ReferenceName)
	}
	if ext.Expr != "#content > p.data" {
		t.Errorf("Expected Expr '#content > p.data', got '%s'", ext.Expr)
	}
	if ext.Attribute != "data-id" {
		t.Errorf("Expected Attribute 'data-id', got '%s'", ext.Attribute)
	}
	if ext.MatchNo != -1 {
		t.Errorf("Expected MatchNo -1, got %d", ext.MatchNo)
	}
}

func TestCssSelectorExtractorExtract(t *testing.T) {
	ext := &domain.HtmlExtractor{
		ReferenceName:     "EXTRACTED_DATA",
		Expr:              "#content > p.data",
		Attribute:         "data-id",
		MatchNo:           -1,
		DefaultValueStr:   "NOT_FOUND",
		DefaultEmptyValue: false,
	}

	body := []byte(`
		<!DOCTYPE html>
		<html>
		<body>
			<div id="content">
			<p class="data" data-id="1">First Value</p>
			<p class="data" data-id="2">Second Value</p>
			</div>
		</body>
		</html>
	`)

	multiRes, ok := ext.ExtractMulti(body)
	if !ok {
		t.Fatalf("Expected ExtractMulti to succeed")
	}

	if multiRes["EXTRACTED_DATA_matchNr"] != "2" {
		t.Errorf("Expected matchNr '2', got '%s'", multiRes["EXTRACTED_DATA_matchNr"])
	}
	if multiRes["EXTRACTED_DATA_1"] != "1" {
		t.Errorf("Expected EXTRACTED_DATA_1 '1', got '%s'", multiRes["EXTRACTED_DATA_1"])
	}
	if multiRes["EXTRACTED_DATA_2"] != "2" {
		t.Errorf("Expected EXTRACTED_DATA_2 '2', got '%s'", multiRes["EXTRACTED_DATA_2"])
	}

	// Test Extract single text
	extText := &domain.HtmlExtractor{
		ReferenceName: "TEXT_DATA",
		Expr:          "#content > p.data",
		Attribute:     "",
		MatchNo:       2,
	}

	res, ok := extText.Extract(body)
	if !ok {
		t.Fatalf("Expected Extract to succeed")
	}
	if res != "Second Value" {
		t.Errorf("Expected 'Second Value', got '%s'", res)
	}
}
