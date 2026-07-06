package threadgroup

import (
	"context"

	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

// Runner defines the strategy for executing a specific type of JMeter Thread Group
type Runner interface {
	Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error
}
