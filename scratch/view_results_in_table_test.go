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

func TestViewResultsInTable(t *testing.T) {
	_ = os.Remove("table_results.csv")
	_ = os.Remove("bin_table.bin")

	// 1. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/listeners/test_view_results_in_table.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	// 2. Validate parsed result collectors
	var foundTable bool
	for _, rc := range plan.ResultCollectors {
		if rc.Filename == "table_results.csv" {
			foundTable = true
			break
		}
	}

	if !foundTable {
		t.Errorf("Did not parse View Results in Table correctly")
	}

	// 3. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_table.bin",
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
	err = usecase.WriteCustomJTLsIfNeeded("bin_table.bin", append(plan.ResultCollectors, plan.ThreadGroups[0].ResultCollectors...))
	if err != nil {
		t.Fatalf("WriteCustomJTLsIfNeeded failed: %v", err)
	}

	// 5. Verify files
	allStat, err := os.Stat("table_results.csv")
	if err != nil || allStat.Size() == 0 {
		t.Errorf("table_results.csv not generated or empty")
	}
}
