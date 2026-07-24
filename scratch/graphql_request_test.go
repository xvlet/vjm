package scratch

import (
	"context"
	"encoding/json"
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

func TestGraphQLRequestSampler(t *testing.T) {
	_ = os.Remove("bin_graphql.bin")

	// 1. Start mock HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/test/sampler/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Errorf("Failed to parse GraphQL request JSON: %v", err)
		}

		if reqBody["query"] != "query getUser($id: ID!) { user(id: $id) { name } }" {
			t.Errorf("Unexpected query: %v", reqBody["query"])
		}

		vars, ok := reqBody["variables"].(map[string]interface{})
		if !ok || vars["id"] != "123" {
			t.Errorf("Unexpected variables: %v", reqBody["variables"])
		}

		if reqBody["operationName"] != "getUser" {
			t.Errorf("Unexpected operationName: %v", reqBody["operationName"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("graphql-success"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port := u.Port()

	// 2. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_graphql_request.jmx")
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
		ResultBinPath: "bin_graphql.bin",
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
	binStat, err := os.Stat("bin_graphql.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_graphql.bin not generated or empty")
	}
}
