package engine

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"
)

// WSRoundTripper intercepts "ws" and "wss" requests and processes them using golang.org/x/net/websocket.
type WSRoundTripper struct {
	Fallback http.RoundTripper
}

func (w *WSRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "ws" || req.URL.Scheme == "wss" {
		origin := "http://localhost"
		if req.URL.Scheme == "wss" {
			origin = "https://localhost"
		}

		wsURL := req.URL.String()
		// Replace ws/wss with the expected URL scheme for websocket.Dial
		// Actually, websocket.Dial expects ws:// or wss://, which req.URL.String() already is.

		ws, err := websocket.Dial(wsURL, "", origin)
		if err != nil {
			return nil, err
		}
		defer func() { _ = ws.Close() }()

		if req.Body != nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil && len(bodyBytes) > 0 {
				_, err = ws.Write(bodyBytes)
				if err != nil {
					return nil, err
				}
			}
		}

		var msg = make([]byte, 8192)
		n, err := ws.Read(msg)
		if err != nil && err.Error() != "EOF" {
			// Some websocket servers close connection after sending message
			if !strings.Contains(err.Error(), "closed") {
				return nil, err
			}
		}

		resp := &http.Response{
			StatusCode:    200,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(msg[:n])),
			ContentLength: int64(n),
			Request:       req,
		}
		return resp, nil
	}

	if w.Fallback == nil {
		w.Fallback = http.DefaultTransport
	}
	return w.Fallback.RoundTrip(req)
}
