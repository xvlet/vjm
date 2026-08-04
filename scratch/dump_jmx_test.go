package scratch

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestDumpJMX(t *testing.T) {
	plan, err := parser.NewDefaultJmxParser().Parse("../tests/samplers/test_complex_sse.jmx")
	if err != nil {
		t.Fatal(err)
	}

	for _, tg := range plan.ThreadGroups {
		fmt.Printf("ThreadGroup: %s (Loops: %d, ContinueForever: %v)\n", tg.Name, tg.Loops, tg.ContinueForever)
		for i, s := range tg.Samplers {
			b, _ := json.Marshal(s)
			fmt.Printf("  Sampler %d: %s\n", i, string(b))
		}
	}
}
