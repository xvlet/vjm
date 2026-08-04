package engine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type sseConn struct {
	resp   *http.Response
	reader *bufio.Reader
}

// SSERoundTripper intercepts "sse" and "sses" requests and handles them as persistent
// SSE connections per session. Connections are cached by URL.
type SSERoundTripper struct {
	Fallback http.RoundTripper
	conns    map[string]*sseConn
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewSSERoundTripper(fallback http.RoundTripper) *SSERoundTripper {
	ctx, cancel := context.WithCancel(context.Background())
	return &SSERoundTripper{
		Fallback: fallback,
		conns:    make(map[string]*sseConn),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// CloseAll closes all cached SSE connections. Should be called via defer
// when the owning session (virtual user) exits to prevent goroutine leaks.
func (s *SSERoundTripper) CloseAll() {
	s.cancel()
	for url, conn := range s.conns {
		if conn.resp != nil && conn.resp.Body != nil {
			_ = conn.resp.Body.Close()
		}
		delete(s.conns, url)
	}
}

func (s *SSERoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "sse" || req.URL.Scheme == "sses" {
		sseURL := req.URL.String()
		ctx := req.Context()

		conn, exists := s.conns[sseURL]
		if !exists {
			// Change scheme back to http/https for actual network request
			realURL := *req.URL
			switch realURL.Scheme {
			case "sse":
				realURL.Scheme = "http"
			case "sses":
				realURL.Scheme = "https"
			}

			// Create a new request based on the original one
			// Use the long-lived session context so the connection isn't closed when the individual request finishes.
			newReq := req.Clone(s.ctx)
			newReq.URL = &realURL
			newReq.Header.Set("Accept", "text/event-stream")
			newReq.Header.Set("Cache-Control", "no-cache")

			fallback := s.Fallback
			if fallback == nil {
				fallback = http.DefaultTransport
			}

			resp, err := fallback.RoundTrip(newReq)
			if err != nil {
				fmt.Printf("[DEBUG-SSE] fallback.RoundTrip returned err: %v\n", err)
				return nil, err
			}

			conn = &sseConn{
				resp:   resp,
				reader: bufio.NewReader(resp.Body),
			}
			s.conns[sseURL] = conn
		}

		// Read one event (separated by \n\n)
		// We use a goroutine to support context cancellation during read
		type readResult struct {
			data []byte
			err  error
		}
		readCh := make(chan readResult, 1)

		go func() {
			var eventBuf bytes.Buffer
			for {
				line, err := conn.reader.ReadBytes('\n')
				if len(line) > 0 {
					eventBuf.Write(line)
				}
				if err != nil {
					readCh <- readResult{data: eventBuf.Bytes(), err: err}
					return
				}
				// Check if the event ends with \n\n
				b := eventBuf.Bytes()
				if len(b) >= 2 && b[len(b)-2] == '\n' && b[len(b)-1] == '\n' {
					readCh <- readResult{data: b, err: nil}
					return
				}
				// Also handle \r\n\r\n
				if len(b) >= 4 && b[len(b)-4] == '\r' && b[len(b)-3] == '\n' && b[len(b)-2] == '\r' && b[len(b)-1] == '\n' {
					readCh <- readResult{data: b, err: nil}
					return
				}
			}
		}()

		select {
		case <-ctx.Done():
			fmt.Printf("[DEBUG-SSE] ctx.Done() triggered! ctx.Err()=%v\n", ctx.Err())
			// Cleanup connection on context timeout/cancel to unblock reader
			if conn.resp != nil && conn.resp.Body != nil {
				_ = conn.resp.Body.Close()
			}
			delete(s.conns, sseURL)
			return nil, ctx.Err()
		case res := <-readCh:
			if res.err != nil {
				if conn.resp != nil && conn.resp.Body != nil {
					_ = conn.resp.Body.Close()
				}
				delete(s.conns, sseURL)
				if len(res.data) == 0 {
					if errors.Is(res.err, io.EOF) {
						return nil, errors.New("EOF - SSE Connection Closed")
					}
					return nil, res.err
				}
			}

			// Wrap the event in a pseudo-HTTP response
			pseudoResp := &http.Response{
				StatusCode:    conn.resp.StatusCode,
				Proto:         conn.resp.Proto,
				ProtoMajor:    conn.resp.ProtoMajor,
				ProtoMinor:    conn.resp.ProtoMinor,
				Header:        conn.resp.Header.Clone(),
				Body:          io.NopCloser(bytes.NewReader(res.data)),
				ContentLength: int64(len(res.data)),
				Request:       req,
			}
			return pseudoResp, nil
		}
	}

	if s.Fallback == nil {
		s.Fallback = http.DefaultTransport
	}
	return s.Fallback.RoundTrip(req)
}
