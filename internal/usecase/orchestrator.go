package usecase

import (
	"context"
	"fmt"
	"log"
	"vjm/internal/domain"
	"vjm/internal/evaluator"
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

func (u *defaultStressTestUsecase) Execute(ctx context.Context, config *domain.TestConfig) error {
	log.Println("[Usecase] Parsing JMX Template...")
	plan, err := u.jmxParser.Parse(config.JmxFilePath)
	if err != nil {
		return fmt.Errorf("failed to parse JMX: %w", err)
	}

	if config.Properties != nil {
		u.evaluator.AddProperties(config.Properties)
	}

	if plan.UserDefinedVariables != nil {
		u.evaluator.AddProperties(plan.UserDefinedVariables)
	}

	log.Println("[Usecase] Executing Vegeta Load Test...")
	err = u.runner.Run(ctx, plan, config, u.evaluator)
	if err != nil {
		return fmt.Errorf("vegeta run failed: %w", err)
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
