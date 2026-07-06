package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

type OpenModelPhase struct {
	StartRate float64 // req/s
	EndRate   float64 // req/s
	Duration  time.Duration
}

type OpenModelPacer struct {
	phases   []OpenModelPhase
	TotalDur time.Duration
}

func ParseOpenModelSchedule(schedule string) (*OpenModelPacer, error) {
	// e.g. "rate(0/sec) random_arrivals(10 sec) rate(10/sec) random_arrivals(10 sec) rate(10/sec)"
	re := regexp.MustCompile(`(rate|random_arrivals|even_arrivals|pause)\s*\(([^)]+)\)`)
	matches := re.FindAllStringSubmatch(schedule, -1)

	var phases []OpenModelPhase
	var currentRate *float64

	parseRate := func(s string) (float64, error) {
		s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
		parts := strings.Split(s, "/")
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid rate format: %s", s)
		}
		val, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		unit := parts[1]
		switch {
		case strings.HasPrefix(unit, "s"):
			return val, nil
		case strings.HasPrefix(unit, "m"):
			return val / 60.0, nil
		case strings.HasPrefix(unit, "h"):
			return val / 3600.0, nil
		}
		return val, nil
	}

	parseDur := func(s string) (time.Duration, error) {
		s = strings.TrimSpace(s)
		parts := strings.Split(s, " ")
		if len(parts) != 2 {
			return time.ParseDuration(strings.ReplaceAll(s, " ", ""))
		}
		val, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		unit := parts[1]
		switch {
		case strings.HasPrefix(unit, "s"):
			return time.Duration(val * float64(time.Second)), nil
		case strings.HasPrefix(unit, "m"):
			return time.Duration(val * float64(time.Minute)), nil
		case strings.HasPrefix(unit, "h"):
			return time.Duration(val * float64(time.Hour)), nil
		}
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}

	for i := 0; i < len(matches); i++ {
		cmd := matches[i][1]
		arg := matches[i][2]

		switch cmd {
		case "rate":
			r, err := parseRate(arg)
			if err != nil {
				return nil, err
			}
			rCopy := r
			currentRate = &rCopy
		case "random_arrivals", "even_arrivals", "pause":
			d, err := parseDur(arg)
			if err != nil {
				return nil, err
			}
			start := 0.0
			if currentRate != nil {
				start = *currentRate
			}

			end := start
			if cmd == "pause" {
				end = 0
				start = 0
			} else if i+1 < len(matches) && matches[i+1][1] == "rate" {
				r, _ := parseRate(matches[i+1][2])
				end = r
			}

			phases = append(phases, OpenModelPhase{StartRate: start, EndRate: end, Duration: d})
			if cmd == "pause" {
				currentRate = &start // 0
			}
		}
	}

	total := time.Duration(0)
	for _, p := range phases {
		total += p.Duration
	}

	if len(phases) == 0 {
		return nil, fmt.Errorf("no valid phases found in schedule")
	}

	return &OpenModelPacer{phases: phases, TotalDur: total}, nil
}

// hitsAt returns the expected number of hits up to time t
func (p *OpenModelPacer) hitsAt(t time.Duration) float64 {
	hits := 0.0
	accumulatedTime := time.Duration(0)

	for _, phase := range p.phases {
		if t <= accumulatedTime {
			break
		}

		phaseT := t - accumulatedTime
		if phaseT > phase.Duration {
			phaseT = phase.Duration
		}

		durSec := phase.Duration.Seconds()
		phaseTSec := phaseT.Seconds()

		if durSec > 0 {
			start := phase.StartRate
			end := phase.EndRate

			h := start*phaseTSec + 0.5*(end-start)*(phaseTSec*phaseTSec)/durSec
			hits += h
		}

		accumulatedTime += phase.Duration
	}

	return hits
}

func (p *OpenModelPacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	if elapsed >= p.TotalDur {
		return 0, true
	}

	expectedHits := p.hitsAt(elapsed)
	if float64(hits) < expectedHits {
		return 0, false
	}

	currentRate := 0.0
	accumulatedTime := time.Duration(0)
	for _, phase := range p.phases {
		if elapsed >= accumulatedTime && elapsed < accumulatedTime+phase.Duration {
			ratio := float64(elapsed-accumulatedTime) / float64(phase.Duration)
			currentRate = phase.StartRate + ratio*(phase.EndRate-phase.StartRate)
			break
		}
		accumulatedTime += phase.Duration
	}

	if currentRate <= 0.001 {
		return 100 * time.Millisecond, false
	}

	// Next hit should be roughly in 1/currentRate seconds
	return time.Duration(float64(time.Second) / currentRate), false
}

// Rate returns the instantaneous rate at the given elapsed time
func (p *OpenModelPacer) Rate(elapsed time.Duration) float64 {
	if elapsed >= p.TotalDur {
		return 0
	}
	accumulatedTime := time.Duration(0)
	for _, phase := range p.phases {
		if elapsed >= accumulatedTime && elapsed < accumulatedTime+phase.Duration {
			ratio := float64(elapsed-accumulatedTime) / float64(phase.Duration)
			return phase.StartRate + ratio*(phase.EndRate-phase.StartRate)
		}
		accumulatedTime += phase.Duration
	}
	return 0
}

// Ensure it implements vegeta.Pacer
var _ vegeta.Pacer = &OpenModelPacer{}
