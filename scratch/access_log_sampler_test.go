package scratch

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
)

func TestAccessLogSampler(t *testing.T) {
	_ = os.Remove("bin_access.bin")

	mockLog := "\"GET /test/sampler/http/get?q=1 HTTP/1.1\"\n\"POST /test/sampler/http/post HTTP/1.1\"\n"
	err := os.WriteFile("access.log", []byte(mockLog), 0644)
	if err != nil {
		t.Fatalf("Failed to create mock access.log: %v", err)
	}
	defer os.Remove("access.log")

	var getCount int32
	var postCount int32

	// 1. Start mock HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received unexpected request: %s %s", r.Method, r.URL.String())
	})
	mux.HandleFunc("/test/sampler/http/get", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received GET request: %s %s", r.Method, r.URL.String())
		if r.Method == "GET" && r.URL.Query().Get("q") == "1" {
			atomic.AddInt32(&getCount, 1)
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/test/sampler/http/post", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received POST request: %s %s", r.Method, r.URL.String())
		if r.Method == "POST" {
			atomic.AddInt32(&postCount, 1)
		}
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port := u.Port()

	// 2. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_access_log_sampler.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	if len(plan.ThreadGroups) > 0 {
		for _, s := range plan.ThreadGroups[0].Samplers {
			if s.IsAccessLogSampler {
				s.AccessLogPort = port
				if parsed, err := url.Parse(s.Request.URL); err == nil {
					parsed.Host = parsed.Hostname() + ":" + port
					s.Request.URL = parsed.String()
				}
			}
		}
	}

	// 3. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_access.bin",
		Rate:          1,
		Duration:      "1s", // Should run 2 loops
	}
	runner := vegeta.NewRunner()

	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)
	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}

	// 4. Verify mock server hits
	if getCount != 1 {
		t.Errorf("Expected 1 GET hit, got %d", getCount)
	}
	if postCount != 1 {
		t.Errorf("Expected 1 POST hit, got %d", postCount)
	}

	// 5. Verify results generated
	binStat, err := os.Stat("bin_access.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_access.bin not generated or empty")
	}
}
