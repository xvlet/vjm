package usecase

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/tsenart/vegeta/v12/lib"
	"github.com/xvlet/vjm/internal/domain"
)

// SaveResponsesIfNeeded parses the vegeta binary results and saves the response bodies
// to individual files according to the ResultSaver configurations.
func SaveResponsesIfNeeded(binPath string, savers []*domain.ResultSaver) error {
	if len(savers) == 0 {
		return nil
	}

	f, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("failed to open bin file for ResultSaver: %w", err)
	}
	defer func() { _ = f.Close() }()

	dec := vegeta.NewDecoder(f)
	seq := 1

	for {
		var res vegeta.Result
		if err := dec.Decode(&res); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode result for ResultSaver: %w", err)
		}

		if len(res.Body) == 0 {
			seq++
			continue
		}

		isError := res.Error != "" || res.Code >= 400
		isSuccess := !isError

		for _, saver := range savers {
			if saver.ErrorsOnly && !isError {
				continue
			}
			if saver.SuccessOnly && !isSuccess {
				continue
			}

			prefix := saver.FilenamePrefix
			if prefix == "" {
				prefix = "response"
			}

			filename := prefix
			if !saver.SkipAutoNumber {
				filename = fmt.Sprintf("%s%d", prefix, seq)
			}
			if !saver.SkipSuffix {
				filename += ".unknown" // Default extension
			}

			// Ensure directory exists if prefix contains path
			dir := filepath.Dir(filename)
			if dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					log.Printf("[WARNING] ResultSaver failed to create dir %s: %v", dir, err)
					continue
				}
			}

			if err := os.WriteFile(filename, res.Body, 0644); err != nil {
				log.Printf("[WARNING] ResultSaver failed to write file %s: %v", filename, err)
			}
		}

		seq++
	}

	log.Printf("[ResultSaver] Finished processing %d savers", len(savers))
	return nil
}
