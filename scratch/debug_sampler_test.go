package scratch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	corevegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
)

func TestDebugSampler(t *testing.T) {
	_ = os.Remove("bin_debug.bin")

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
	plan, err := p.Parse("../tests/samplers/test_debug_sampler.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) > 0 {
		for _, s := range plan.ThreadGroups[0].Samplers {
			if s.Request != nil {
				s.Request.URL = strings.Replace(s.Request.URL, "58080", port, 1)
			}
		}
	}

	// 4. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_debug.bin",
		Rate:          1,
		Duration:      "1s",
	}
	runner := vegeta.NewRunner()

	vars := map[string]string{
		"MY_TEST_VAR": "TEST_VALUE_123",
	}
	eval := evaluator.NewDefaultEvaluator(vars)
	eval.SetVariable("RUNTIME_VAR", "RUNTIME_VAL")

	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}

	// 5. Verify results
	f, err := os.Open("bin_debug.bin")
	if err != nil {
		t.Fatalf("Failed to open bin_debug.bin: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	dec := corevegeta.NewDecoder(f)
	foundDebug := false
	for {
		var res corevegeta.Result
		if err := dec.Decode(&res); err != nil {
			break
		}
		if res.URL == "DebugSampler" {
			foundDebug = true
		}
	}
	if !foundDebug {
		t.Errorf("DebugSampler result not found in bin file")
	}

	// 6. Verify results
	binStat, err := os.Stat("bin_debug.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_debug.bin not generated or empty")
	}
}
