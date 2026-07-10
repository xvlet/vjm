package vegeta

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
	"github.com/xvlet/vjm/internal/infra/vegeta/threadgroup"
)

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator) error {
	if plan == nil || len(plan.ThreadGroups) == 0 {
		return fmt.Errorf("test plan is empty or has no thread groups")
	}

	totalSamplers := 0
	for _, tg := range plan.ThreadGroups {
		totalSamplers += len(tg.Samplers)
	}
	if totalSamplers == 0 {
		return fmt.Errorf("no HTTP requests found in thread groups")
	}

	// Remove result file if exists to start fresh
	_ = os.Remove(config.ResultBinPath)

	if config.ForceCLI {
		log.Printf("[VegetaRunner] -force-cli flag enabled. Ignoring Thread Group configs and using Rate=%d, Duration=%s", config.Rate, config.Duration)
		tgRunner := &threadgroup.StandardRunner{}
		return tgRunner.Run(ctx, plan, config, eval.Clone())
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.ThreadGroups))
	tgPaths := make([]string, len(plan.ThreadGroups))

	for i, tg := range plan.ThreadGroups {
		wg.Add(1)
		go func(idx int, threadGrp *domain.ThreadGroup) {
			defer wg.Done()

			subPlan := &domain.TestPlan{
				Name:                 plan.Name,
				ThreadGroups:         []*domain.ThreadGroup{threadGrp},
				CSVDataSets:          plan.CSVDataSets,
				CookieManager:        plan.CookieManager,
				CacheManager:         plan.CacheManager,
				DNSCacheManager:      plan.DNSCacheManager,
				AuthManager:          plan.AuthManager,
				Counters:             plan.Counters,
				RandomVariables:      plan.RandomVariables,
				UserDefinedVariables: plan.UserDefinedVariables,
			}

			subConfig := *config
			subConfig.ResultBinPath = fmt.Sprintf("%s.tg%d", config.ResultBinPath, idx)

			tgRunner := threadgroup.GetRunner(threadGrp)

			if err := tgRunner.Run(ctx, subPlan, &subConfig, eval.Clone()); err != nil {
				errCh <- err
			} else {
				tgPaths[idx] = subConfig.ResultBinPath
			}
		}(i, tg)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	// Merge all parts into final result
	out, err := os.OpenFile(config.ResultBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	enc := vegeta.NewEncoder(out)
	for _, paths := range tgPaths {
		for _, partPath := range strings.Split(paths, ",") {
			partPath = strings.TrimSpace(partPath)
			if partPath == "" {
				continue
			}
			in, err := os.Open(partPath)
			if err == nil {
				dec := vegeta.NewDecoder(in)
				var res vegeta.Result
				for {
					if err := dec.Decode(&res); err != nil {
						break
					}
					_ = enc.Encode(&res)
				}
				_ = in.Close()
				_ = os.Remove(partPath)
			}
		}
	}

	return nil
}
