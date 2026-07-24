package scratch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
)

func TestOSProcessSampler(t *testing.T) {
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_os_process_sampler.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No thread groups found")
	}
	tg := plan.ThreadGroups[0]

	if len(tg.Samplers) == 0 {
		t.Fatalf("No samplers found in thread group")
	}

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runner := vegeta.NewRunner()
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_os_process.bin",
		Rate:          1,
		Duration:      "1s",
		Workers:       1,
	}

	err = runner.Run(ctx, plan, cfg, eval)
	if err != nil {
		t.Fatalf("Runner failed: %v", err)
	}

	fmt.Println("Test OS Process Sampler finished successfully.")
}
