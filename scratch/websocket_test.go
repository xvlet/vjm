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

	"golang.org/x/net/websocket"
)

func wsEchoServer(ws *websocket.Conn) {
	_, _ = io.Copy(ws, ws)
}

func TestWebSocket(t *testing.T) {
	_ = os.Remove("bin_ws.bin")

	// 1. Start mock websocket server
	http.Handle("/ws", websocket.Handler(wsEchoServer))
	server := httptest.NewServer(nil)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port := u.Port()

	// 2. Parse JMX
	p := parser.NewDefaultJmxParser()
	plan, err := p.Parse("../tests/samplers/test_websocket.jmx")
	if err != nil {
		t.Fatalf("Failed to parse JMX: %v", err)
	}

	// Inject the mock server port into the parsed plan
	if len(plan.ThreadGroups) > 0 && len(plan.ThreadGroups[0].Samplers) > 0 {
		req := plan.ThreadGroups[0].Samplers[0].Request
		req.URL = strings.Replace(req.URL, ":58081", ":"+port, 1)
	}

	// 3. Run mock engine
	cfg := &domain.TestConfig{
		ResultBinPath: "bin_ws.bin",
		Rate:          1000,
		Duration:      "1s", // maxhits limits to 1
	}
	runner := vegeta.NewRunner()
	eval := evaluator.NewDefaultEvaluator(plan.UserDefinedVariables)

	err = runner.Run(context.Background(), plan, cfg, eval)
	if err != nil {
		t.Fatalf("Engine run failed: %v", err)
	}

	// 4. Verify results
	binStat, err := os.Stat("bin_ws.bin")
	if err != nil || binStat.Size() == 0 {
		t.Errorf("bin_ws.bin not generated or empty")
	}
}
