package engine

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// WSRoundTripper intercepts "ws" and "wss" requests and handles them as persistent
// connections per session. Connections are cached by URL and reused across multiple
// requests within the same virtual user session.
type WSRoundTripper struct {
	Fallback http.RoundTripper
	conns    map[string]*websocket.Conn
}

func NewWSRoundTripper(fallback http.RoundTripper) *WSRoundTripper {
	return &WSRoundTripper{
		Fallback: fallback,
		conns:    make(map[string]*websocket.Conn),
	}
}

// CloseAll closes all cached WebSocket connections. Should be called via defer
// when the owning session (virtual user) exits to prevent goroutine leaks.
func (w *WSRoundTripper) CloseAll() {
	for url, conn := range w.conns {
		_ = conn.Close()
		delete(w.conns, url)
	}
}

func (w *WSRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "ws" || req.URL.Scheme == "wss" {
		wsURL := req.URL.String()
		ctx := req.Context()

		ws, exists := w.conns[wsURL]
		if !exists {
			origin := "http://localhost"
			if req.URL.Scheme == "wss" {
				origin = "https://localhost"
			}

			wsConfig, err := websocket.NewConfig(wsURL, origin)
			if err != nil {
				return nil, err
			}
			// Copy all request headers (Cookie, Authorization, etc.) to the WebSocket config.
			wsConfig.Header = req.Header.Clone()

			ws, err = websocket.DialConfig(wsConfig)
			if err != nil {
				return nil, err
			}
			w.conns[wsURL] = ws
		}

		if req.Body != nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil && len(bodyBytes) > 0 {
				_, err = ws.Write(bodyBytes)
				if err != nil {
					_ = ws.Close()
					delete(w.conns, wsURL)
					return nil, err
				}
			}
		}

		// Read a single message/frame. Use a cancellation goroutine to unblock
		// ws.Read when the request context is cancelled (e.g. at end of attack duration).
		readDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				// Immediately expire the read deadline to unblock ws.Read.
				_ = ws.SetReadDeadline(time.Now())
			case <-readDone:
			}
		}()

		var msgBuf bytes.Buffer
		tmp := make([]byte, 32*1024)
		n, err := ws.Read(tmp)
		close(readDone) // signal the cancellation goroutine to exit

		if n > 0 {
			msgBuf.Write(tmp[:n])
		}
		if err != nil {
			_ = ws.Close()
			delete(w.conns, wsURL)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != io.EOF && !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "i/o timeout") {
				return nil, err
			}
		}
		msg := msgBuf.Bytes()

		resp := &http.Response{
			StatusCode:    200,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(msg)),
			ContentLength: int64(len(msg)),
			Request:       req,
		}
		return resp, nil
	}

	if w.Fallback == nil {
		w.Fallback = http.DefaultTransport
	}
	return w.Fallback.RoundTrip(req)
}
