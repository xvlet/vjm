package scratch

import (
	"fmt"
	"regexp"
	"testing"
)

func TestRegex(t *testing.T) {
	reqLineRegex := regexp.MustCompile(`"([A-Z]+)\s+([^ ]+)\s+HTTP/.*"`)
	lines := []string{
		`127.0.0.1 - - [23/Jul/2026:10:00:00 +0900] "GET /test/sampler/http/get?q=1 HTTP/1.1" 200 1234`,
		`127.0.0.1 - - [23/Jul/2026:10:00:01 +0900] "POST /test/sampler/http/post HTTP/1.1" 200 1234`,
	}
	for _, line := range lines {
		matches := reqLineRegex.FindStringSubmatch(line)
		fmt.Printf("Matches: %q\n", matches)
	}
}
