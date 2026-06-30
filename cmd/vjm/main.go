package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"vjm/internal/domain"
	"vjm/internal/evaluator"
	"vjm/internal/infra/jmeter"
	"vjm/internal/infra/parser"
	"vjm/internal/infra/vegeta"
	"vjm/internal/usecase"
)

func main() {
	jmxPath := flag.String("t", "", "JMeter .jmx file path")
	propFiles := flag.String("q", "", "Comma separated list of .properties files")
	rate := flag.Int("rate", 1000, "TPS Rate")
	duration := flag.String("duration", "30s", "Duration (e.g. 30s, 1m)")
	workers := flag.Int("workers", 0, "Max workers (0 means vegeta default)")
	resultFile := flag.String("l", "result.bin", "Vegeta binary result file")
	reportDir := flag.String("e", "", "HTML Report output directory")
	jmeterHome := flag.String("jmeter-home", os.Getenv("JMETER_HOME"), "JMETER_HOME path")

	flag.Parse()

	if *jmxPath == "" {
		fmt.Println("Usage: vjm -t <plan.jmx> [-q props.properties] -rate 3000 -duration 60s")
		os.Exit(1)
	}

	// 1. Parse Properties
	props := make(map[string]string)
	if *propFiles != "" {
		files := strings.Split(*propFiles, ",")
		for _, f := range files {
			p, err := parser.LoadProperties(strings.TrimSpace(f))
			if err != nil {
				log.Printf("Warning: failed to load properties %s: %v", f, err)
				continue
			}
			for k, v := range p {
				props[k] = v // Merge
			}
		}
	}

	config := &domain.TestConfig{
		JmxFilePath:   *jmxPath,
		Properties:    props,
		Rate:          *rate,
		Duration:      *duration,
		Workers:       *workers,
		ResultBinPath: *resultFile,
		ResultJtlPath: strings.TrimSuffix(*resultFile, ".bin") + ".jtl",
		ReportDirPath: *reportDir,
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
