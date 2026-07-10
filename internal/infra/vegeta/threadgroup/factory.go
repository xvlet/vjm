package threadgroup

import (
	"github.com/xvlet/vjm/internal/domain"
)

// GetRunner returns the appropriate Runner for the given thread group
func GetRunner(tg *domain.ThreadGroup) Runner {
	if tg.UltimateConfig != nil {
		return &UltimateRunner{}
	}
	if tg.SteppingConfig != nil {
		return &SteppingRunner{}
	}
	if tg.FreeFormArrivalsConfig != nil {
		return &FreeFormArrivalsRunner{}
	}
	if tg.ArrivalsConfig != nil {
		return &ArrivalsRunner{}
	}
	if tg.ConcurrencyConfig != nil {
		return &ConcurrencyRunner{}
	}
	if tg.OpenModelSchedule != "" {
		return &OpenModelRunner{}
	}
	return &StandardRunner{}
}
