package usecase

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
)

func WriteCustomJTLsIfNeeded(binPath string, collectors []*domain.ResultCollector) error {
	var activeCollectors []*domain.ResultCollector
	for _, c := range collectors {
		if c.Filename != "" {
			activeCollectors = append(activeCollectors, c)
		}
	}
	if len(activeCollectors) == 0 {
		return nil
	}

	type writerContext struct {
		file   *os.File
		writer *csv.Writer
		c      *domain.ResultCollector
	}

	var contexts []*writerContext
	for _, c := range activeCollectors {
		f, err := os.Create(c.Filename)
		if err != nil {
			// Clean up opened files on error
			for _, ctx := range contexts {
				_ = ctx.file.Close()
			}
			return fmt.Errorf("failed to create custom JTL file %s: %w", c.Filename, err)
		}
		w := csv.NewWriter(f)
		header := []string{
			"timeStamp", "elapsed", "label", "responseCode", "responseMessage",
			"threadName", "dataType", "success", "failureMessage", "bytes",
			"sentBytes", "grpThreads", "allThreads", "URL", "Latency",
			"IdleTime", "Connect",
		}
		if err := w.Write(header); err != nil {
			_ = f.Close()
			return fmt.Errorf("failed to write CSV header for %s: %w", c.Filename, err)
		}

		contexts = append(contexts, &writerContext{
			file:   f,
			writer: w,
			c:      c,
		})
	}

	defer func() {
		for _, ctx := range contexts {
			ctx.writer.Flush()
			_ = ctx.file.Close()
		}
	}()

	paths := []string{}
	for _, p := range strings.Split(binPath, ",") {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, strings.TrimSpace(p))
		}
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

			isSuccess := false
			if res.Code >= 200 && res.Code < 400 && res.Error == "" {
				isSuccess = true
			}
			successStr := "false"
			if isSuccess {
				successStr = "true"
			}
			msg := "OK"
			if res.Error != "" {
				msg = res.Error
			}

			label := res.Attack
			if label == "" {
				label = "HTTP_Request"
			}

			record := []string{
				strconv.FormatInt(ts, 10),
				strconv.FormatInt(lat, 10),
				label,
				codeStr, msg, "Vegeta-1-1", "text", successStr, res.Error,
				strconv.FormatUint(res.BytesIn, 10), strconv.FormatUint(res.BytesOut, 10), "1", "1", res.URL, strconv.FormatInt(lat, 10), "0", "0",
			}

			for _, ctx := range contexts {
				if ctx.c.ErrorLogging && isSuccess {
					continue
				}
				if ctx.c.SuccessOnlyLogging && !isSuccess {
					continue
				}
				if err := ctx.writer.Write(record); err != nil {
					log.Printf("failed to write record to CSV %s: %v", ctx.c.Filename, err)
				}
			}
		}
		_ = f.Close()
	}

	return nil
}
