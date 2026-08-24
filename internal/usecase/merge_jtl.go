package usecase

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type MergeJtlUsecase struct{}

func NewMergeJtlUsecase() *MergeJtlUsecase {
	return &MergeJtlUsecase{}
}

// jtlRow represents a parsed line from a JTL file
type jtlRow struct {
	timestamp int64
	line      string
}

func (u *MergeJtlUsecase) Execute(outputFile string, inputFiles []string) error {
	if len(inputFiles) == 0 {
		return fmt.Errorf("no input files provided")
	}

	var header string
	var rows []jtlRow

	for i, inputFile := range inputFiles {
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", inputFile, err)
		}
		defer func() { _ = file.Close() }()

		scanner := bufio.NewScanner(file)
		isFirstLine := true

		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			if isFirstLine {
				isFirstLine = false
				if i == 0 {
					header = line // Capture header from the first file
				}
				continue // Skip header for all files
			}

			// Parse timestamp (usually the first column in JMeter JTLs)
			parts := strings.SplitN(line, ",", 2)
			if len(parts) > 0 {
				ts, err := strconv.ParseInt(parts[0], 10, 64)
				if err == nil {
					rows = append(rows, jtlRow{timestamp: ts, line: line})
				} else {
					// Fallback to 0 if parsing fails, still keep the line
					rows = append(rows, jtlRow{timestamp: 0, line: line})
				}
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading file %s: %w", inputFile, err)
		}
	}

	// Sort rows by timestamp
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].timestamp < rows[j].timestamp
	})

	// Write to output file
	out, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", outputFile, err)
	}
	defer func() { _ = out.Close() }()

	writer := bufio.NewWriter(out)

	// Write header
	if header != "" {
		if _, err := writer.WriteString(header + "\n"); err != nil {
			return err
		}
	}

	// Write sorted rows
	for _, row := range rows {
		if _, err := writer.WriteString(row.line + "\n"); err != nil {
			return err
		}
	}

	return writer.Flush()
}
