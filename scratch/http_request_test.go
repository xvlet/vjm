package scratch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
)

func TestHTTPRequestSampler(t *testing.T) {
	_ = os.Remove("bin_http.bin")

	// 1. Start mock HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/test/sampler/http/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("get-success"))
	})
	mux.HandleFunc("/test/sampler/http/post", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		contentType := r.Header.Get("Content-Type")
		if contentType != "text/plain" {
			t.Errorf("Expected Content-Type text/plain, got %s", contentType)
		}
		customHeader := r.Header.Get("Custom-Header")
		if customHeader != "vjm-test" {
			t.Errorf("Expected Custom-Header vjm-test, got %s", customHeader)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "post-body-data" {
			t.Errorf("Expected body 'post-body-data', got '%s'", string(body))
		}

		w.Header().Set("echo-post-status", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("post-success"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port := u.Port()

	// 2. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_http_request.jmx")
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
		ResultBinPath: "bin_http.bin",
		Rate:          10,
		Duration:      "1s", // maxhits limits to 1
	}
	runner := vegeta.NewRunner()
	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)

	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}

	// 4. Verify results
	binStat, err := os.Stat("bin_http.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_http.bin not generated or empty")
	}
}
