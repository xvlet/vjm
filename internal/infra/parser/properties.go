package parser

import (
	"bufio"
	"os"
	"strings"
)

// LoadProperties reads a Java-style .properties file and returns a key-value map.
func LoadProperties(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	props := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Java properties: leading whitespace is removed
		line := strings.TrimLeft(scanner.Text(), " \t\f")
		// Skip empty lines and comments
		if len(line) == 0 || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		eqIdx := strings.Index(line, "=")
		colIdx := strings.Index(line, ":")
		spaceIdx := strings.IndexAny(line, " \t\f")

		splitIdx := -1
		if eqIdx != -1 {
			splitIdx = eqIdx
		}
		if colIdx != -1 && (splitIdx == -1 || colIdx < splitIdx) {
			splitIdx = colIdx
		}
		if spaceIdx != -1 && (splitIdx == -1 || spaceIdx < splitIdx) {
			splitIdx = spaceIdx
		}

		if splitIdx != -1 {
			key := line[:splitIdx]
			// Trim leading whitespace and the first '=' or ':' from the value part
			valueStr := line[splitIdx:]
			valueStr = strings.TrimLeft(valueStr, " \t\f")
			if len(valueStr) > 0 && (valueStr[0] == '=' || valueStr[0] == ':') {
				valueStr = valueStr[1:]
			}
			valueStr = strings.TrimLeft(valueStr, " \t\f")
			props[key] = valueStr
		} else {
			props[line] = "" // Key only, empty value
		}
	}
	return props, scanner.Err()
}
