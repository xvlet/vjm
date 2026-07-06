package threadgroup

import (
	"github.com/xvlet/vjm/internal/domain"
)

// GetRunner returns the appropriate Runner for the given thread group
func GetRunner(tg *domain.ThreadGroup) Runner {
	if tg.SteppingConfig != nil {
		return &SteppingRunner{}
	}
	if tg.ConcurrencyConfig != nil {
		return &ConcurrencyRunner{}
	}
	if tg.OpenModelSchedule != "" {
		return &OpenModelRunner{}
	}
	return &StandardRunner{}
}
