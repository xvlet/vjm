package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJmxParserSnapshots(t *testing.T) {
	// Find all .jmx files in ../../../tests
	var jmxFiles []string
	err := filepath.Walk("../../../tests", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jmx") {
			jmxFiles = append(jmxFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk tests dir: %v", err)
	}

	parser := NewDefaultJmxParser()

	for _, jmxFile := range jmxFiles {
		t.Run(filepath.Base(jmxFile), func(t *testing.T) {
			plan, err := parser.Parse(jmxFile)
			if err != nil {
				t.Fatalf("Failed to parse %s: %v", jmxFile, err)
			}

			// Serialize to JSON with indentation
			planJSON, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				t.Fatalf("Failed to marshal plan to JSON: %v", err)
			}

			snapshotFile := jmxFile + ".snapshot.json"

			// Check if snapshot exists
			if _, err := os.Stat(snapshotFile); os.IsNotExist(err) {
				// Create snapshot if it doesn't exist (update mode)
				err = os.WriteFile(snapshotFile, planJSON, 0644)
				if err != nil {
					t.Fatalf("Failed to write snapshot: %v", err)
				}
				t.Logf("Created snapshot for %s", jmxFile)
			} else {
				// Compare with existing snapshot
				snapshotData, err := os.ReadFile(snapshotFile)
				if err != nil {
					t.Fatalf("Failed to read snapshot: %v", err)
				}

				if string(planJSON) != string(snapshotData) {
					// Output diff or just fail
					t.Errorf("Mismatch for %s.\nExpected (Snapshot):\n%s\n\nActual (Parsed):\n%s\n", jmxFile, string(snapshotData), string(planJSON))
				}
			}
		})
	}
}
