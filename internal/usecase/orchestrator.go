package usecase

import (
	"context"
	"fmt"
	"log"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

type defaultStressTestUsecase struct {
	jmxParser JmxParser
	runner    VegetaRunner
	reporter  JmeterReporter
	evaluator evaluator.Evaluator
}

func NewStressTestUsecase(jmx JmxParser, runner VegetaRunner, rep JmeterReporter, eval evaluator.Evaluator) StressTestUsecase {
	return &defaultStressTestUsecase{
		jmxParser: jmx,
		runner:    runner,
		reporter:  rep,
		evaluator: eval,
	}
}

// NewReportOnlyUsecase creates a lightweight usecase for report generation only.
// Calling Execute() on this instance will return an error.
func NewReportOnlyUsecase(rep JmeterReporter) StressTestUsecase {
	return &defaultStressTestUsecase{reporter: rep}
}

func (u *defaultStressTestUsecase) Execute(ctx context.Context, config *domain.TestConfig) error {
	// Guard against misconfigured report-only instances
	if u.jmxParser == nil || u.runner == nil {
		return fmt.Errorf("Execute requires a fully configured usecase; use NewStressTestUsecase")
	}

	log.Println("[Usecase] Parsing JMX Template...")
	plan, err := u.jmxParser.Parse(config.JmxFilePath)
	if err != nil {
		return fmt.Errorf("failed to parse JMX: %w", err)
	}

	if config.Properties != nil {
		u.evaluator.AddProperties(config.Properties)
	}

	if plan.UserDefinedVariables != nil {
		u.evaluator.AddVariables(plan.UserDefinedVariables)
	}

	log.Println("[Usecase] Executing Vegeta Load Test...")
	err = u.runner.Run(ctx, plan, config, u.evaluator)
	if err != nil {
		return fmt.Errorf("vegeta run failed: %w", err)
	}

	if err := u.reporter.PrintReport(config.ResultBinPath); err != nil {
		log.Printf("[Usecase] Warning: Failed to print vegeta report: %v", err)
	}

	log.Println("[Usecase] Converting Bin to JTL...")
	err = u.reporter.ConvertToJTL(config.ResultBinPath, config.ResultJtlPath)
	if err != nil {
		return fmt.Errorf("JTL conversion failed: %w", err)
	}

	if config.ReportDirPath != "" {
		log.Println("[Usecase] Generating HTML Report...")
		err = u.reporter.GenerateHTML(config.ResultJtlPath, config.ReportDirPath, 1000)
		if err != nil {
			log.Printf("[WARNING] HTML report generation failed: %v", err)
		}
	}

	log.Println("[Usecase] Stress Test flow completed successfully!")
	return nil
}

func (u *defaultStressTestUsecase) GenerateReportOnly(inputPath string, reportDirPath string) error {
	isJtl := len(inputPath) > 4 && inputPath[len(inputPath)-4:] == ".jtl"
	jtlPath := inputPath

	if !isJtl {
		if len(inputPath) > 4 && inputPath[len(inputPath)-4:] == ".bin" {
			jtlPath = inputPath[:len(inputPath)-4] + ".jtl"
		} else {
			jtlPath = inputPath + ".jtl"
		}

		log.Printf("[Usecase] Converting Bin (%s) to JTL (%s)...", inputPath, jtlPath)
		err := u.reporter.ConvertToJTL(inputPath, jtlPath)
		if err != nil {
			return fmt.Errorf("JTL conversion failed: %w", err)
		}
	} else {
		log.Printf("[Usecase] JTL file provided directly (%s), skipping bin conversion.", jtlPath)
	}

	if reportDirPath == "" {
		log.Println("[Usecase] No report directory specified (-e). Skipping HTML report generation.")
		log.Printf("[Usecase] JTL file is ready at: %s", jtlPath)
		log.Println("[Usecase] Report generation flow completed successfully!")
		return nil
	}

	log.Printf("[Usecase] Generating HTML Report to %s...", reportDirPath)
	if err := u.reporter.GenerateHTML(jtlPath, reportDirPath, 1000); err != nil {
		return fmt.Errorf("HTML report generation failed: %w", err)
	}

	log.Println("[Usecase] Report generation flow completed successfully!")
	return nil
}
