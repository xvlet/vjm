package vegeta

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
	"github.com/xvlet/vjm/internal/domain"
	"github.com/xvlet/vjm/internal/evaluator"
)

type Target struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Body   []byte              `json:"body,omitempty"`
	Header map[string][]string `json:"header,omitempty"`
}

type Runner struct {
}

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

	vegetaPath, err := exec.LookPath("vegeta")
	if err != nil {
		return fmt.Errorf("vegeta command not found: %w", err)
	}

	args := []string{
		"attack",
		"-format=json",
		"-targets=stdin",
		"-lazy=true",
		"-rate", fmt.Sprintf("%d", config.Rate),
		"-duration", config.Duration,
		"-keepalive=true",
	}
	if config.Workers > 0 {
		args = append(args, "-max-workers", fmt.Sprintf("%d", config.Workers))
	}

	cmd := exec.CommandContext(ctx, vegetaPath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	outFile, err := os.Create(config.ResultBinPath)
	if err != nil {
		return fmt.Errorf("failed to create result file: %w", err)
	}
	defer func() { _ = outFile.Close() }()
	
	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	log.Println("[VegetaRunner] Starting vegeta attack...")
	log.Printf("[VegetaRunner] Rate     : %d TPS", config.Rate)
	log.Printf("[VegetaRunner] Duration : %s", config.Duration)
	log.Printf("[VegetaRunner] Workers  : %d", config.Workers)
	attackStart := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start vegeta: %w", err)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[VegetaRunner] Panic in generation goroutine: %v", r)
			}
		}()
		defer func() { _ = stdin.Close() }()
		encoder := json.NewEncoder(stdin)
		
		var count int
		for {
			select {
			case <-ctx.Done():
				return
			default:
				for _, tg := range plan.ThreadGroups {
					for _, sampler := range tg.Samplers {
						reqTemplate := sampler.Request
						if reqTemplate == nil {
							continue
						}
						
						evalURL := eval.Evaluate(reqTemplate.URL)
						evalBody := eval.Evaluate(reqTemplate.BodyTemplate)
						evalHeaders := make(map[string][]string)
						for k, v := range reqTemplate.Headers {
							evalHeaders[k] = []string{eval.Evaluate(v)}
						}

						target := Target{
							Method: reqTemplate.Method,
							URL:    evalURL,
							Header: evalHeaders,
						}
						if evalBody != "" {
							target.Body = []byte(evalBody)
						}

						if count == 0 {
							log.Printf("[DEBUG] Method=%s", target.Method)
							log.Printf("[DEBUG] URL=%s", target.URL)
							for hk, hv := range target.Header {
								log.Printf("[DEBUG] Header [%s] = %s", hk, hv[0])
							}
							log.Printf("[DEBUG] BodyLen=%d", len(target.Body))
						}
						count++

						if err := encoder.Encode(target); err != nil {
							// Broken pipe means vegeta closed stdin (finished)
							return
						}
					}
				}
			}
		}
	}()

	err = cmd.Wait()
	elapsed := time.Since(attackStart).Round(time.Millisecond)
	log.Printf("[VegetaRunner] Attack completed in %s. Results saved to %s", elapsed, config.ResultBinPath)
	if err != nil {
		// Remove incomplete result file to avoid corrupting future report runs
		_ = outFile.Close()
		_ = os.Remove(config.ResultBinPath)
		return fmt.Errorf("vegeta attack failed: %w", err)
	}
	return nil
}
