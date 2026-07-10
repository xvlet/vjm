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

func (r *Reporter) ConvertToJTL(binPath, jtlPath string) error {
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
	header := []string{
		"timeStamp", "elapsed", "label", "responseCode", "responseMessage",
		"threadName", "dataType", "success", "failureMessage", "bytes",
		"sentBytes", "grpThreads", "allThreads", "URL", "Latency",
		"IdleTime", "Connect",
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

			label := "HTTP_Request"

			_ = writer.Write([]string{
				strconv.FormatInt(ts, 10),
				strconv.FormatInt(lat, 10),
				label,
				codeStr, msg, "Vegeta-1-1", "text", success, res.Error,
				strconv.FormatUint(res.BytesIn, 10), strconv.FormatUint(res.BytesOut, 10), "1", "1", res.URL, strconv.FormatInt(lat, 10), "0", "0",
			})
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

	// JMeter fails if the output directory already exists; remove it first.
	if _, err := os.Stat(reportDir); err == nil {
		if err := os.RemoveAll(reportDir); err != nil {
			return fmt.Errorf("failed to remove existing report dir %s: %w", reportDir, err)
		}
	}

	log.Printf("[JmeterReporter] Generating HTML report to %s...", reportDir)
	return cmd.Run()
}
