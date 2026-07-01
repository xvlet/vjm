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
)

type Reporter struct {
	jmeterHome string
}

func NewReporter(jmeterHome string) *Reporter {
	return &Reporter{jmeterHome: jmeterHome}
}

func (r *Reporter) PrintReport(binPath string) error {
	vegetaPath, err := exec.LookPath("vegeta")
	if err != nil {
		return fmt.Errorf("vegeta command not found: %w", err)
	}

	cmd := exec.Command(vegetaPath, "report", binPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Println("===================================================")
	log.Println("Vegeta Attack Report:")
	log.Println("===================================================")
	err = cmd.Run()
	log.Println("===================================================")
	return err
}

func (r *Reporter) ConvertToJTL(binPath, jtlPath string) error {
	vegetaPath, err := exec.LookPath("vegeta")
	if err != nil {
		return fmt.Errorf("vegeta command not found: %w", err)
	}

	cmd := exec.Command(vegetaPath, "encode", "-to", "csv", binPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
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

	reader := csv.NewReader(stdout)
	reader.FieldsPerRecord = -1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if _, ok := err.(*csv.ParseError); ok {
				log.Printf("[WARN] Skipping invalid CSV record: %v", err)
				continue
			}
			return fmt.Errorf("csv read error: %w", err)
		}
		if len(record) < 8 {
			continue
		}

		timestampNs, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			continue
		}

		ts := timestampNs / 1000000
		codeStr := record[1]
		latencyNs, _ := strconv.ParseInt(record[2], 10, 64)
		lat := latencyNs / 1000000
		sentStr := record[3]
		recvStr := record[4]
		errStr := record[5]
		// body is record[6], headers is record[7]
		
		// Decode vegeta csv format: unix_timestamp_ns, status_code, latency_ns, bytes_out, bytes_in, error, body, name, seq, method, url, headers
		urlStr := ""
		if len(record) > 10 {
			urlStr = record[10]
		}
		
		code, _ := strconv.Atoi(codeStr)
		success := "false"
		if code >= 200 && code < 400 && errStr == "" {
			success = "true"
		}
		msg := "OK"
		if errStr != "" {
			msg = errStr
		}

		// Use a dynamic label if possible, but vegeta doesn't easily embed Sampler Name in standard CSV unless we hack it.
		// For now we use "HTTP_Request" or extract from target.
		label := "HTTP_Request"

		_ = writer.Write([]string{
			strconv.FormatInt(ts, 10),
			strconv.FormatInt(lat, 10), // elapsed (using latency for now)
			label,
			codeStr, msg, "Vegeta-1-1", "text", success, errStr,
			recvStr, sentStr, "1", "1", urlStr, strconv.FormatInt(lat, 10), "0", "0",
		})
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("JTL write error: %w", err)
	}
	return cmd.Wait()
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
