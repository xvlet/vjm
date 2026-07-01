package usecase

import (
	"context"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

type StressTestUsecase interface {
	Execute(ctx context.Context, config *domain.TestConfig) error
	GenerateReportOnly(binPath string, reportDirPath string) error
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
	PrintReport(binPath string) error
}
