package scratch

import (
	"context"
	"os"
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
	"github.com/xvlet/vjm/internal/usecase"
)

func TestSimpleDataWriter(t *testing.T) {
	_ = os.Remove("writer_all.csv")
	_ = os.Remove("writer_err.csv")
	_ = os.Remove("writer_succ.csv")
	_ = os.Remove("bin_sdw.bin")

	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_simple_data_writer.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	// 2. Validate parsed result collectors
	var foundAll, foundErr, foundSucc bool
	for _, rc := range plan.ResultCollectors {
		switch rc.Filename {
		case "writer_all.csv":
			foundAll = true
		case "writer_err.csv":
			foundErr = true
			if !rc.ErrorLogging {
				t.Errorf("writer_err.csv should have ErrorLogging=true")
			}
		case "writer_succ.csv":
			foundSucc = true
			if !rc.SuccessOnlyLogging {
				t.Errorf("writer_succ.csv should have SuccessOnlyLogging=true")
			}
		}
	}

	if !foundAll || !foundErr || !foundSucc {
		t.Errorf("Did not parse all Simple Data Writers correctly")
	}

	// 3. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_sdw.bin",
		Rate:          1000,
		Duration:      "1s", // Engine override shouldn't matter since maxhits limits to 2 requests
	}
	runner := vegeta.NewRunner()
	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)

	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}

	// 4. Trigger ResultWriterService directly
	err = usecase.WriteCustomJTLsIfNeeded("bin_sdw.bin", append(plan.ResultCollectors, plan.ThreadGroups[0].ResultCollectors...))
	if err != nil {
		t.Fatalf("WriteCustomJTLsIfNeeded failed: %v", err)
	}

	// 5. Verify files
	allStat, err := os.Stat("writer_all.csv")
	if err != nil || allStat.Size() == 0 {
		t.Errorf("writer_all.csv not generated or empty")
	}

	_, err = os.Stat("writer_err.csv")
	if err != nil {
		t.Errorf("writer_err.csv not generated")
	}

	_, err = os.Stat("writer_succ.csv")
	if err != nil {
		t.Errorf("writer_succ.csv not generated")
	}

	// Mock server /test returns N/A (500 internal server error? or 200 OK?)
	// Depends on mock server. But files should exist with at least the header.
}
