package jmeter

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
)

type Reporter struct {
	jmeterHome string
}

func NewReporter(jmeterHome string) *Reporter {
	return &Reporter{jmeterHome: jmeterHome}
}

func (r *Reporter) PrintReport(binPath string) error {
	paths := []string{}
	for _, p := range strings.Split(binPath, ",") {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, strings.TrimSpace(p))
		}
	}

	var metrics vegeta.Metrics
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		dec := vegeta.NewDecoder(f)
		for {
			var res vegeta.Result
			if err := dec.Decode(&res); err != nil {
				if err == io.EOF {
					break
				}
				_ = f.Close()
				return err
			}
			metrics.Add(&res)
		}
		_ = f.Close()
	}
	metrics.Close()

	log.Println("===================================================")
	log.Println("Vegeta Attack Report:")
	log.Println("===================================================")
	reporter := vegeta.NewTextReporter(&metrics)
	err := reporter.Report(os.Stdout)
	log.Println("===================================================")
	return err
}

func (r *Reporter) ConvertToJTL(plan *domain.TestPlan, binPath, jtlPath string) error {
	paths := []string{}
	for _, p := range strings.Split(binPath, ",") {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, strings.TrimSpace(p))
		}
	}

	outFile, err := os.Create(jtlPath)
	if err != nil {
		return err
	}
	defer func() { _ = outFile.Close() }()

	writer := csv.NewWriter(outFile)

	// Determine configuration
	config := map[string]bool{
		"timestamp":      true,
		"time":           true,
		"label":          true,
		"code":           true,
		"message":        true,
		"threadName":     true,
		"dataType":       true,
		"success":        true,
		"failureMessage": true,
		"bytes":          true,
		"sentBytes":      true,
		"threadCounts":   true,
		"url":            true,
		"latency":        true,
		"idleTime":       true,
		"connectTime":    true,
	}

	if plan != nil && len(plan.ResultCollectors) > 0 {
		for _, rc := range plan.ResultCollectors {
			if len(rc.Configuration) > 0 {
				config = rc.Configuration
				break
			}
		}
	}

	header := []string{}
	if config["timestamp"] {
		header = append(header, "timeStamp")
	}
	if config["time"] {
		header = append(header, "elapsed")
	}
	if config["label"] {
		header = append(header, "label")
	}
	if config["code"] {
		header = append(header, "responseCode")
	}
	if config["message"] {
		header = append(header, "responseMessage")
	}
	if config["threadName"] {
		header = append(header, "threadName")
	}
	if config["dataType"] {
		header = append(header, "dataType")
	}
	if config["success"] {
		header = append(header, "success")
	}
	if config["failureMessage"] {
		header = append(header, "failureMessage")
	}
	if config["bytes"] {
		header = append(header, "bytes")
	}
	if config["sentBytes"] {
		header = append(header, "sentBytes")
	}
	if config["threadCounts"] {
		header = append(header, "grpThreads", "allThreads")
	}
	if config["url"] {
		header = append(header, "URL")
	}
	if config["latency"] {
		header = append(header, "Latency")
	}
	if config["idleTime"] {
		header = append(header, "IdleTime")
	}
	if config["connectTime"] {
		header = append(header, "Connect")
	}

	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write JTL header: %w", err)
	}

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		dec := vegeta.NewDecoder(f)
		for {
			var res vegeta.Result
			if err := dec.Decode(&res); err != nil {
				if err == io.EOF {
					break
				}
				_ = f.Close()
				return err
			}

			ts := res.Timestamp.UnixNano() / 1000000
			lat := res.Latency.Nanoseconds() / 1000000
			codeStr := strconv.Itoa(int(res.Code))

			success := "false"
			if res.Code >= 200 && res.Code < 400 && res.Error == "" {
				success = "true"
			}
			msg := "OK"
			if res.Error != "" {
				msg = res.Error
			}

			label := res.Attack
			if label == "" {
				label = "HTTP_Request"
			}

			row := []string{}
			if config["timestamp"] {
				row = append(row, strconv.FormatInt(ts, 10))
			}
			if config["time"] {
				row = append(row, strconv.FormatInt(lat, 10))
			} // Using latency as elapsed time for now
			if config["label"] {
				row = append(row, label)
			}
			if config["code"] {
				row = append(row, codeStr)
			}
			if config["message"] {
				row = append(row, msg)
			}
			if config["threadName"] {
				row = append(row, "Vegeta-1-1")
			}
			if config["dataType"] {
				row = append(row, "text")
			}
			if config["success"] {
				row = append(row, success)
			}
			if config["failureMessage"] {
				row = append(row, res.Error)
			}
			if config["bytes"] {
				row = append(row, strconv.FormatUint(res.BytesIn, 10))
			}
			if config["sentBytes"] {
				row = append(row, strconv.FormatUint(res.BytesOut, 10))
			}
			if config["threadCounts"] {
				row = append(row, "1", "1")
			}
			if config["url"] {
				row = append(row, res.URL)
			}
			if config["latency"] {
				row = append(row, strconv.FormatInt(lat, 10))
			}
			if config["idleTime"] {
				row = append(row, "0")
			}
			if config["connectTime"] {
				row = append(row, "0")
			}

			if err := writer.Write(row); err != nil {
				log.Printf("[JmeterReporter] Warning: failed to write JTL row for %s: %v", res.URL, err)
			}
		}
		_ = f.Close()
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("JTL write error: %w", err)
	}
	return nil
}

func (r *Reporter) GenerateHTML(jtlPath, reportDir string, granularity int) error {
	if r.jmeterHome == "" {
		return fmt.Errorf("JMETER_HOME is not set")
	}

	jmeterBin := filepath.Join(r.jmeterHome, "bin", "jmeter")
	if _, err := os.Stat(jmeterBin); os.IsNotExist(err) {
		jmeterBin += ".sh"
		if _, err := os.Stat(jmeterBin); os.IsNotExist(err) {
			return fmt.Errorf("jmeter executable not found at %s", r.jmeterHome)
		}
	}

	cmd := exec.Command(jmeterBin,
		"-Jjmeter.reportgenerator.overall_granularity="+strconv.Itoa(granularity),
		"-g", jtlPath,
		"-o", reportDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Ensure the parent directory exists, as JMeter will fail if it doesn't.
	parentDir := filepath.Dir(reportDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for report: %w", err)
	}

	// JMeter fails if the output directory already exists; remove it first.
	if _, err := os.Stat(reportDir); err == nil {
		if err := os.RemoveAll(reportDir); err != nil {
			return fmt.Errorf("failed to remove existing report dir %s: %w", reportDir, err)
		}
	}

	log.Printf("[JmeterReporter] Generating HTML report to %s...", reportDir)
	return cmd.Run()
}

func (r *Reporter) CopyResult(src, dst string) error {
	if src == dst {
		return nil
	}
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()

	_, err = io.Copy(destination, source)
	return err
}
