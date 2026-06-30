package usecase

import (
	"context"
	"vjm/internal/domain"
	"vjm/internal/evaluator"
)

type StressTestUsecase interface {
	Execute(ctx context.Context, config *domain.TestConfig) error
}

type JmxParser interface {
	Parse(filePath string) (*domain.TestPlan, error)
}

type VegetaRunner interface {
	Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error
}

type JmeterReporter interface {
	ConvertToJTL(binPath, jtlPath string) error
	GenerateHTML(jtlPath, reportDir string, granularity int) error
}
