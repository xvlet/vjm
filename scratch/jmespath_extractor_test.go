package scratch

import (
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestJMESPathExtractorParse(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/post-processors/test_jmespath_extractor.jmx")
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

	ext, ok := req1.Extractors[0].(*domain.JMESPathExtractor)
	if !ok {
		t.Fatalf("Req1 Extractor is not JMESPathExtractor")
	}

	if ext.ReferenceName != "JMES_TITLE" {
		t.Errorf("Expected ReferenceName 'JMES_TITLE', got '%s'", ext.ReferenceName)
	}
	if ext.JmesPathExpr != "store.book[*].title" {
		t.Errorf("Expected JmesPathExpr 'store.book[*].title', got '%s'", ext.JmesPathExpr)
	}
	if ext.MatchNo != -1 {
		t.Errorf("Expected MatchNo -1, got %d", ext.MatchNo)
	}
}

func TestJMESPathExtractorExtract(t *testing.T) {
	ext := &domain.JMESPathExtractor{
		ReferenceName:   "JMES_TITLE",
		JmesPathExpr:    "store.book[*].title",
		MatchNo:         -1,
		DefaultValueStr: "NOT_FOUND",
	}

	body := []byte(`
		{
			"store": {
				"book": [
					{"category": "reference", "author": "Nigel Rees", "title": "Sayings of the Century", "price": 8.95},
					{"category": "fiction", "author": "Herman Melville", "title": "Moby Dick", "price": 8.99}
				]
			}
		}
	`)

	multiRes, ok := ext.ExtractMulti(body)
	if !ok {
		t.Fatalf("Expected ExtractMulti to succeed")
	}

	if multiRes["JMES_TITLE_matchNr"] != "2" {
		t.Errorf("Expected matchNr '2', got '%s'", multiRes["JMES_TITLE_matchNr"])
	}
	if multiRes["JMES_TITLE_1"] != "Sayings of the Century" {
		t.Errorf("Expected JMES_TITLE_1 'Sayings of the Century', got '%s'", multiRes["JMES_TITLE_1"])
	}
	if multiRes["JMES_TITLE_2"] != "Moby Dick" {
		t.Errorf("Expected JMES_TITLE_2 'Moby Dick', got '%s'", multiRes["JMES_TITLE_2"])
	}

	// Test Extract single text
	extSingle := &domain.JMESPathExtractor{
		ReferenceName: "JMES_SINGLE",
		JmesPathExpr:  "store.book[0].author",
		MatchNo:       1,
	}

	res, ok := extSingle.Extract(body)
	if !ok {
		t.Fatalf("Expected Extract to succeed")
	}
	if res != "Nigel Rees" {
		t.Errorf("Expected 'Nigel Rees', got '%s'", res)
	}
}
