package scratch

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xvlet/vjm/internal/infra/parser"
)

func TestParser(t *testing.T) {
	p := parser.NewDefaultJmxParser()

	// Get project root to find the jmx file
	_, filename, _, _ := runtime.Caller(0)
	rootDir := filepath.Join(filepath.Dir(filename), "..")
	jmxPath := filepath.Join(rootDir, "tests/logic-controller/test_foreach_controller.jmx")

	plan, err := p.Parse(jmxPath)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(plan.ThreadGroups) == 0 {
		t.Fatalf("No ThreadGroups found")
	}

	for i, s := range plan.ThreadGroups[0].Samplers {
		reqName := s.Name
		fmt.Printf("Sampler %d: Control=%v Type=%s InputVal=%q ReturnVal=%q UseSep=%v ReqName=%s\n",
			i, s.IsControlFlow, s.ControlType, s.ForEachInputVal, s.ForEachReturnVal, s.ForEachUseSeparator, reqName)
	}
}
