package engine

import (
	"bufio"
	"context"
	"log"
	"os"
	"regexp"
	"sync"
)

type AccessLogEntry struct {
	Method string
	Path   string
}

// AccessLogStreamer streams parsed access log entries into a buffered channel.
type AccessLogStreamer struct {
	C        <-chan AccessLogEntry
	filename string
	once     sync.Once
	ch       chan AccessLogEntry
}

func (a *StatefulAttacker) getAccessLogStreamer(ctx context.Context, filename string, workers uint64) *AccessLogStreamer {
	bufSize := workers
	if bufSize < 100 {
		bufSize = 100
	} else if bufSize > 10000 {
		bufSize = 10000
	}

	actual, _ := a.accessLogStreamers.LoadOrStore(filename, &AccessLogStreamer{
		filename: filename,
		ch:       make(chan AccessLogEntry, bufSize),
	})

	streamer := actual.(*AccessLogStreamer)
	streamer.C = streamer.ch

	streamer.once.Do(func() {
		go streamer.streamLogFile(ctx)
	})

	return streamer
}

// Regex to parse typical apache common log format request line
var reqLineRegex = regexp.MustCompile(`"([A-Z]+)\s+([^ ]+)\s+HTTP/.*"`)

func (s *AccessLogStreamer) streamLogFile(ctx context.Context) {
	defer close(s.ch)

	for {
		f, err := os.Open(s.filename)
		if err != nil {
			log.Printf("[AccessLogStreamer] Error opening log file %s: %v", s.filename, err)
			return
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			matches := reqLineRegex.FindStringSubmatch(line)
			if len(matches) >= 3 {
				entry := AccessLogEntry{
					Method: matches[1],
					Path:   matches[2],
				}
				// Will block if the channel buffer is full, providing backpressure naturally
				select {
				case s.ch <- entry:
				case <-ctx.Done():
					_ = f.Close()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("[AccessLogStreamer] Error reading log file %s: %v", s.filename, err)
		}

		_ = f.Close()

		// If EOF reached, we restart reading from the top (JMeter AccessLogSampler reuses file)
		// Or we could break if we don't want to loop. For load testing, usually looping is desired.
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
