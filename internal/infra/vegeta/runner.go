package vegeta

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	var steppingCfg *domain.SteppingConfig
	for _, tg := range plan.ThreadGroups {
		totalSamplers += len(tg.Samplers)
		if tg.SteppingConfig != nil && steppingCfg == nil {
			steppingCfg = tg.SteppingConfig
		}
	}
	if totalSamplers == 0 {
		return fmt.Errorf("no HTTP requests found in thread groups")
	}

	vegetaPath, err := exec.LookPath("vegeta")
	if err != nil {
		return fmt.Errorf("vegeta command not found: %w", err)
	}

	// Remove result file if exists to start fresh
	_ = os.Remove(config.ResultBinPath)

	if steppingCfg != nil && !config.ForceCLI {
		return r.runStepping(ctx, plan, config, eval, vegetaPath, steppingCfg)
	}

	if steppingCfg != nil && config.ForceCLI {
		log.Printf("[VegetaRunner] -force-cli flag enabled. Ignoring Thread Group config and using Rate=%d, Duration=%s", config.Rate, config.Duration)
	}

	return r.runSingle(ctx, plan, config, eval, vegetaPath, config.Rate, config.Duration)
}

func (r *Runner) runStepping(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator, vegetaPath string, stepCfg *domain.SteppingConfig) error {
	// Evaluate strings to ints
	maxRate, _ := strconv.Atoi(eval.Evaluate(stepCfg.MaxRate))
	stepRate, _ := strconv.Atoi(eval.Evaluate(stepCfg.StepRate))
	
	initDelaySec, _ := strconv.Atoi(eval.Evaluate(stepCfg.InitialDelay))
	stepDurSec, _ := strconv.Atoi(eval.Evaluate(stepCfg.StepDuration))
	holdDurSec, _ := strconv.Atoi(eval.Evaluate(stepCfg.HoldDuration))

	log.Printf("[VegetaRunner] Found SteppingThreadGroup config. MaxRate: %d, StepRate: %d", maxRate, stepRate)

	if initDelaySec > 0 {
		log.Printf("[VegetaRunner] Initial delay for %ds", initDelaySec)
		select {
		case <-time.After(time.Duration(initDelaySec) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	currentRate := 0
	if stepRate <= 0 {
		stepRate = maxRate // Prevent infinite loop if 0
	}

	var binPaths []string
	stepIndex := 1
	baseBinPath := config.ResultBinPath

	for currentRate < maxRate {
		currentRate += stepRate
		if currentRate > maxRate {
			currentRate = maxRate
		}

		durationStr := fmt.Sprintf("%ds", stepDurSec)
		log.Printf("[VegetaRunner] --- Stepping: Running at %d TPS for %s ---", currentRate, durationStr)
		
		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		err := r.runSingle(ctx, plan, config, eval, vegetaPath, currentRate, durationStr)
		if err != nil {
			return err
		}
		stepIndex++
	}

	if holdDurSec > 0 {
		durationStr := fmt.Sprintf("%ds", holdDurSec)
		log.Printf("[VegetaRunner] --- Stepping: Holding Max Rate %d TPS for %s ---", maxRate, durationStr)

		stepBinPath := fmt.Sprintf("%s.%d", baseBinPath, stepIndex)
		binPaths = append(binPaths, stepBinPath)
		config.ResultBinPath = stepBinPath

		err := r.runSingle(ctx, plan, config, eval, vegetaPath, maxRate, durationStr)
		if err != nil {
			return err
		}
	}

	config.ResultBinPath = strings.Join(binPaths, ",")
	return nil
}

func (r *Runner) runSingle(ctx context.Context, plan *domain.TestPlan, config *domain.TestConfig, eval evaluator.Evaluator, vegetaPath string, rate int, duration string) error {
	args := []string{
		"attack",
		"-format=json",
		"-targets=stdin",
		"-lazy=true",
		"-rate", fmt.Sprintf("%d", rate),
		"-duration", duration,
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

	// Open in TRUNC mode so it starts fresh (we no longer append gob streams)
	outFile, err := os.OpenFile(config.ResultBinPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open result file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	// Wrap outputs in large buffers to minimize syscalls and prevent blocking vegeta attack
	bufferedOut := bufio.NewWriterSize(outFile, 1024*1024) // 1MB buffer
	
	encodeCmd := exec.CommandContext(ctx, vegetaPath, "encode", "-to", "json")
	encodeStdin, err := encodeCmd.StdinPipe()
	if err != nil {
		return err
	}
	bufferedEncode := bufio.NewWriterSize(encodeStdin, 1024*1024) // 1MB buffer

	encodeStdout, err := encodeCmd.StdoutPipe()
	if err != nil {
		return err
	}

	cmd.Stdout = io.MultiWriter(bufferedOut, bufferedEncode)
	cmd.Stderr = os.Stderr

	if err := encodeCmd.Start(); err != nil {
		return err
	}

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

						if err := encoder.Encode(target); err != nil {
							// Broken pipe means vegeta closed stdin (finished)
							return
						}
					}
				}
			}
		}
	}()

	dashboardDone := make(chan struct{})
	go r.runDashboard(encodeStdout, dashboardDone)

	err = cmd.Wait()
	_ = bufferedOut.Flush()
	_ = bufferedEncode.Flush()
	_ = encodeStdin.Close() // this tells encodeCmd to stop
	_ = encodeCmd.Wait()
	<-dashboardDone
	
	elapsed := time.Since(attackStart).Round(time.Millisecond)
	log.Printf("[VegetaRunner] Step completed in %s.", elapsed)
	if err != nil {
		return fmt.Errorf("vegeta attack failed: %w", err)
	}
	return nil
}

// fast parsing to avoid encoding/json overhead
func parseFastJSON(line []byte) (code int, lat int64, hasErr bool) {
	latIdx := bytes.Index(line, []byte(`"latency":`))
	if latIdx != -1 {
		latIdx += 10
		end := bytes.IndexByte(line[latIdx:], ',')
		if end != -1 {
			lat, _ = strconv.ParseInt(string(line[latIdx:latIdx+end]), 10, 64)
		}
	}
	
	codeIdx := bytes.Index(line, []byte(`"code":`))
	if codeIdx != -1 {
		codeIdx += 7
		end := bytes.IndexByte(line[codeIdx:], ',')
		if end != -1 {
			code, _ = strconv.Atoi(string(line[codeIdx:codeIdx+end]))
		}
	}

	errIdx := bytes.Index(line, []byte(`"error":"`))
	if errIdx != -1 {
		errIdx += 9
		if errIdx < len(line) && line[errIdx] != '"' {
			hasErr = true
		}
	}
	if code >= 400 || code == 0 {
		hasErr = true
	}
	return
}

func (r *Runner) runDashboard(stdout io.Reader, done chan struct{}) {
	var (
		intervalReqs    atomic.Int64
		intervalLatency atomic.Int64
		intervalErrors  atomic.Int64
		totalReqs       atomic.Int64
	)
	var mu sync.Mutex
	latencies := make([]float64, 0, 10000)

	startTime := time.Now()
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			_, lat, hasErr := parseFastJSON(scanner.Bytes())

			intervalReqs.Add(1)
			totalReqs.Add(1)
			intervalLatency.Add(lat)

			latMs := float64(lat) / 1e6
			mu.Lock()
			latencies = append(latencies, latMs)
			mu.Unlock()

			if hasErr {
				intervalErrors.Add(1)
			}
		}
	}()

	for {
		select {
		case <-done:
			ticker.Stop()
			return
		case <-ticker.C:
			iReqs := intervalReqs.Swap(0)
			iLat := intervalLatency.Swap(0)
			iErrs := intervalErrors.Swap(0)
			tReqs := totalReqs.Load()

			var currentLatencies []float64
			mu.Lock()
			currentLatencies = latencies
			latencies = make([]float64, 0, 10000)
			mu.Unlock()

			p99 := 0.0
			maxLat := 0.0
			if len(currentLatencies) > 0 {
				sort.Float64s(currentLatencies)
				maxLat = currentLatencies[len(currentLatencies)-1]
				p99Idx := int(float64(len(currentLatencies)) * 0.99)
				if p99Idx >= len(currentLatencies) {
					p99Idx = len(currentLatencies) - 1
				}
				p99 = currentLatencies[p99Idx]
			}

			tps := float64(iReqs) / 5.0
			avgLatMs := 0.0
			errPct := 0.0
			if iReqs > 0 {
				avgLatMs = (float64(iLat) / float64(iReqs)) / 1e6
				errPct = (float64(iErrs) / float64(iReqs)) * 100.0
			}

			elapsed := time.Since(startTime).Round(time.Second)
			log.Printf("[Dashboard] %02d:%02d | TPS: %5.1f | Avg: %5.1fms | P99: %5.1fms | Max: %5.1fms | Err: %3.1f%% | TotReq: %d",
				int(elapsed.Minutes()), int(elapsed.Seconds())%60, tps, avgLatMs, p99, maxLat, errPct, tReqs)
		}
	}
}
