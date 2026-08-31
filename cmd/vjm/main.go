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

var Version = "dev"

// multiFlag allows a flag to be specified multiple times
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func handleMerge(args []string) {
	mergeCmd := flag.NewFlagSet("merge", flag.ExitOnError)
	output := mergeCmd.String("o", "", "Output JTL file path")

	mergeCmd.Usage = func() {
		fmt.Println("Usage: vjm merge -o <output.jtl> <input1.jtl> <input2.jtl> ...")
		mergeCmd.PrintDefaults()
	}

	if err := mergeCmd.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing merge arguments: %v\n", err)
		os.Exit(1)
	}

	inputs := mergeCmd.Args()

	if *output == "" {
		fmt.Println("Error: -o (output file) is required")
		mergeCmd.Usage()
		os.Exit(1)
	}

	if len(inputs) == 0 {
		fmt.Println("Error: No input files provided")
		mergeCmd.Usage()
		os.Exit(1)
	}

	uc := usecase.NewMergeJtlUsecase()
	err := uc.Execute(*output, inputs)
	if err != nil {
		log.Fatalf("Merge failed: %v", err)
	}
	fmt.Printf("Merge successfully completed: %s\n", *output)
}

func main() {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "merge":
			handleMerge(os.Args[2:]) // Pass from os.Args[2:] so FlagSet can parse
			return
		}
	}

	jmxPath := flag.String("t", "", "JMeter .jmx file path")

	rate := flag.Int("rate", 0, "TPS Rate (0 means unthrottled or use JMX settings)")
	flag.IntVar(rate, "r", 0, "TPS Rate (alias for -rate)")

	duration := flag.String("duration", "", "Duration (e.g. 30s, 1m). Empty means use JMX settings")
	flag.StringVar(duration, "d", "", "Duration (alias for -duration)")

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

	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.BoolVar(versionFlag, "v", false, "Print version information (alias for -version)")

	flag.Usage = func() {
		fmt.Printf("Vegeta JMeter Engine (vjm) %s\n", Version)
		fmt.Println("A high-performance HTTP load testing tool bridging JMeter templates and Vegeta core.")
		fmt.Println()
		fmt.Println("Usage: vjm -t <plan.jmx> [-p props1.properties] [-p props2.properties] -r 3000 -d 60s")
		fmt.Println("       vjm -g <result.bin> -e <report_dir>")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -t string")
		fmt.Println("        JMeter .jmx file path (required for test execution)")
		fmt.Println("  -r, -rate int")
		fmt.Println("        TPS Rate (default 0: follow JMX ThroughputTimer or unthrottled)")
		fmt.Println("  -d, -duration string")
		fmt.Println("        Duration (e.g. 30s, 1m) (default \"\": follow JMX Scheduler)")
		fmt.Println("  -w, -workers int")
		fmt.Println("        Max workers (default 0: follow JMX NumThreads)")
		fmt.Println("  -p value")
		fmt.Println("        Properties file (can be specified multiple times)")
		fmt.Println("  -l string")
		fmt.Println("        Vegeta binary result file (defaults to results/result_YYYYMMDD_HHMMSS.bin)")
		fmt.Println("  -e, -export string")
		fmt.Println("        HTML Report output directory")
		fmt.Println("  -g, -report-only string")
		fmt.Println("        Generate report only from an existing .bin or .jtl file")
		fmt.Println("  -f, -force-cli")
		fmt.Println("        Force conversion of complex Thread Groups (Stepping, Ultimate, etc.) into Standard Thread Groups")
		fmt.Printf("  -jmeter-home string\n        JMETER_HOME path (default %q)\n", os.Getenv("JMETER_HOME"))
		fmt.Println("  -v, -version")
		fmt.Println("        Print version information and exit")
		fmt.Println()
		fmt.Println("Docs & Homepage: https://vjm-cli.pages.dev")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("vjm version %s\n", Version)
		fmt.Printf("(Docs & Homepage: https://vjm-cli.pages.dev/)\n")
		os.Exit(0)
	}

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
		_ = os.MkdirAll("results", 0750)
		finalResultBin = filepath.Join("results", "result_"+timestamp+".bin")
	} else {
		// Ensure parent directories exist for custom result file path
		_ = os.MkdirAll(filepath.Dir(finalResultBin), 0750)
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
