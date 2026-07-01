package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/jmeter"
	"github.com/xvlet/vjm/internal/infra/parser"
	"github.com/xvlet/vjm/internal/infra/vegeta"
	"github.com/xvlet/vjm/internal/usecase"
)

// multiFlag allows a flag to be specified multiple times
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	jmxPath := flag.String("t", "", "JMeter .jmx file path")
	
	rate := flag.Int("rate", 1000, "TPS Rate")
	flag.IntVar(rate, "r", 1000, "TPS Rate (alias for -rate)")
	
	duration := flag.String("duration", "30s", "Duration (e.g. 30s, 1m)")
	flag.StringVar(duration, "d", "30s", "Duration (alias for -duration)")
	
	workers := flag.Int("workers", 0, "Max workers (0 means vegeta default)")
	flag.IntVar(workers, "w", 0, "Max workers (alias for -workers)")
	
	resultFile := flag.String("l", "", "Vegeta binary result file (defaults to results/result_YYYYMMDD_HHMMSS.bin)")
	
	reportDir := flag.String("export", "", "HTML Report output directory")
	flag.StringVar(reportDir, "e", "", "HTML Report output directory (alias for -export)")
	
	jmeterHome := flag.String("jmeter-home", os.Getenv("JMETER_HOME"), "JMETER_HOME path")

	var propFiles multiFlag
	flag.Var(&propFiles, "p", "Properties file (can be specified multiple times)")

	reportOnly := flag.String("report-only", "", "Generate report only from an existing .bin file")
	flag.StringVar(reportOnly, "g", "", "Generate report only (alias for -report-only)")

	forceCLI := flag.Bool("force-cli", false, "Force CLI rate and duration, ignoring JMX Thread Group configuration")
	flag.BoolVar(forceCLI, "f", false, "Force CLI rate (alias for -force-cli)")

	flag.Usage = func() {
		fmt.Println("Vegeta JMeter Engine (vjm) v1.0")
		fmt.Println("A high-performance HTTP load testing tool bridging JMeter templates and Vegeta core.")
		fmt.Println()
		fmt.Println("Usage: vjm -t <plan.jmx> [-p props1.properties] [-p props2.properties] -r 3000 -d 60s")
		fmt.Println("       vjm -g <result.bin> -e <report_dir>")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -t string")
		fmt.Println("        JMeter .jmx file path")
		fmt.Println("  -r, -rate int")
		fmt.Println("        TPS Rate (default 1000)")
		fmt.Println("  -d, -duration string")
		fmt.Println("        Duration (e.g. 30s, 1m) (default \"30s\")")
		fmt.Println("  -w, -workers int")
		fmt.Println("        Max workers (0 means vegeta default)")
		fmt.Println("  -p value")
		fmt.Println("        Properties file (can be specified multiple times)")
		fmt.Println("  -l string")
		fmt.Println("        Vegeta binary result file (defaults to results/result_YYYYMMDD_HHMMSS.bin)")
		fmt.Println("  -e, -export string")
		fmt.Println("        HTML Report output directory")
		fmt.Println("  -g, -report-only string")
		fmt.Println("        Generate report only from an existing .bin file")
		fmt.Println("  -f, -force-cli")
		fmt.Println("        Force CLI rate and duration, ignoring JMX Thread Group configuration")
		fmt.Printf("  -jmeter-home string\n        JMETER_HOME path (default %q)\n", os.Getenv("JMETER_HOME"))
	}

	flag.Parse()

	// 1. Check if it's report-only mode
	if *reportOnly != "" {
		if *reportDir == "" {
			log.Fatal("-e (export directory) is required when using -g / -report-only")
		}
		reporter := jmeter.NewReporter(*jmeterHome)
		uc := usecase.NewReportOnlyUsecase(reporter)
		err := uc.GenerateReportOnly(*reportOnly, *reportDir)
		if err != nil {
			log.Fatalf("Report generation failed: %v", err)
		}
		os.Exit(0)
	}

	if *jmxPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	// 1. Parse Properties
	props := make(map[string]string)
	for _, f := range propFiles {
		p, err := parser.LoadProperties(strings.TrimSpace(f))
		if err != nil {
			log.Printf("Warning: failed to load properties %s: %v", f, err)
			continue
		}
		for k, v := range p {
			props[k] = v // Merge
		}
	}

	timestamp := time.Now().Format("20060102_150405")

	finalResultBin := *resultFile
	if finalResultBin == "" {
		_ = os.MkdirAll("results", 0755)
		finalResultBin = filepath.Join("results", "result_"+timestamp+".bin")
	} else {
		// Ensure parent directories exist for custom result file path
		_ = os.MkdirAll(filepath.Dir(finalResultBin), 0755)
	}

	var finalReportDir string
	if *reportDir != "" {
		finalReportDir = filepath.Join(*reportDir, "report_"+timestamp)
	}

	config := &domain.TestConfig{
		JmxFilePath:   *jmxPath,
		Properties:    props,
		Rate:          *rate,
		Duration:      *duration,
		Workers:       *workers,
		ResultBinPath: finalResultBin,
		ResultJtlPath: strings.TrimSuffix(finalResultBin, ".bin") + ".jtl",
		ReportDirPath: finalReportDir,
		ForceCLI:      *forceCLI,
	}

	// 2. DI Setup
	jmxParser := parser.NewDefaultJmxParser()
	runner := vegeta.NewRunner()
	reporter := jmeter.NewReporter(*jmeterHome)

	eval := evaluator.NewDefaultEvaluator(nil)

	uc := usecase.NewStressTestUsecase(jmxParser, runner, reporter, eval)

	// 3. Execute
	ctx := context.Background()
	if err := uc.Execute(ctx, config); err != nil {
		log.Fatalf("Test Failed: %v", err)
	}
}
