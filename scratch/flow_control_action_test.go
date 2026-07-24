package scratch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
)

func TestFlowControlActionSampler(t *testing.T) {
	_ = os.Remove("bin_flow.bin")

	// 1. Start mock HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/test/sampler/http/get", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port := u.Port()

	// 2. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_flow_control_action.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	// Inject the mock server port into the parsed plan
	if len(plan.ThreadGroups) > 0 {
		for _, s := range plan.ThreadGroups[0].Samplers {
			if s.Request != nil {
				s.Request.URL = strings.Replace(s.Request.URL, "58080", port, 1)
			}
		}
	}

	// 3. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_flow.bin",
		Rate:          1,
		Duration:      "1s",
	}

	if len(plan.ThreadGroups) > 0 {
		plan.ThreadGroups[0].Loops = 2
	}
	runner := vegeta.NewRunner()
	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)

	start := time.Now()
	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}
	duration := time.Since(start)

	// Flow Control Action duration is 500ms
	if duration < 500*time.Millisecond {
		t.Errorf("Flow Control Action pause was not respected, execution took %v", duration)
	}

	// Verify results
	binStat, err := os.Stat("bin_flow.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_flow.bin not generated or empty")
	}
}
