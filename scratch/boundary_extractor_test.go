package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestBoundaryExtractorParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_boundary_extractor.jmx")
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

	ext, ok := req1.Extractors[0].(*domain.BoundaryExtractor)
	if !ok {
		t.Fatalf("Req1 Extractor is not BoundaryExtractor")
	}

	if ext.ReferenceName != "EXTRACTED_DATA" {
		t.Errorf("Expected ReferenceName 'EXTRACTED_DATA', got '%s'", ext.ReferenceName)
	}
	if ext.LBoundary != "name=\"" {
		t.Errorf("Expected LBoundary 'name=\"', got '%s'", ext.LBoundary)
	}
	if ext.RBoundary != "\"" {
		t.Errorf("Expected RBoundary '\"', got '%s'", ext.RBoundary)
	}
	if ext.MatchNo != -1 {
		t.Errorf("Expected MatchNo -1, got %d", ext.MatchNo)
	}
	if ext.DefaultValueStr != "NOT_FOUND" {
		t.Errorf("Expected DefaultValueStr 'NOT_FOUND', got '%s'", ext.DefaultValueStr)
	}
	if ext.DefaultEmptyValue != false {
		t.Errorf("Expected DefaultEmptyValue false, got %v", ext.DefaultEmptyValue)
	}
}

func TestBoundaryExtractorExtract(t *testing.T) {
	ext := &domain.BoundaryExtractor{
		ReferenceName:     "EXTRACTED_DATA",
		LBoundary:         "name=\"",
		RBoundary:         "\"",
		MatchNo:           -1,
		DefaultValueStr:   "NOT_FOUND",
		DefaultEmptyValue: false,
	}

	body := []byte(`name="John Doe";age="30";name="Jane Doe";`)

	multiRes, ok := ext.ExtractMulti(body)
	if !ok {
		t.Fatalf("Expected ExtractMulti to succeed")
	}

	if multiRes["EXTRACTED_DATA_matchNr"] != "2" {
		t.Errorf("Expected matchNr '2', got '%s'", multiRes["EXTRACTED_DATA_matchNr"])
	}
	if multiRes["EXTRACTED_DATA_1"] != "John Doe" {
		t.Errorf("Expected EXTRACTED_DATA_1 'John Doe', got '%s'", multiRes["EXTRACTED_DATA_1"])
	}
	if multiRes["EXTRACTED_DATA_2"] != "Jane Doe" {
		t.Errorf("Expected EXTRACTED_DATA_2 'Jane Doe', got '%s'", multiRes["EXTRACTED_DATA_2"])
	}

	// Test Extract single
	extSingle := &domain.BoundaryExtractor{
		ReferenceName: "SINGLE_DATA",
		LBoundary:     "name=\"",
		RBoundary:     "\"",
		MatchNo:       2,
	}

	res, ok := extSingle.Extract(body)
	if !ok {
		t.Fatalf("Expected Extract to succeed")
	}
	if res != "Jane Doe" {
		t.Errorf("Expected 'Jane Doe', got '%s'", res)
	}
}
