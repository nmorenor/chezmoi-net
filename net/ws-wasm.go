//go:build js
// +build js

package net

import (
	"context"
	"crypto/tls"
	"net/http"

	"nhooyr.io/websocket"
)

// Note: You cant inject tlsConfig here, you are required to use the tlsConfiguration as defined by the browser.
func dialWs(ctx context.Context, url string, tlsConfig *tls.Config) (*websocket.Conn, error) {
	headers := http.Header{}
	// Retrieve headers from the context
	if ctxHeaders, ok := ctx.Value(connectionHeadersContextKey).(*map[string]string); ok && ctxHeaders != nil {
		for key, value := range *ctxHeaders {
			headers.Add(key, value)
		}
	}
	wsConn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	wsConn.SetReadLimit(150000)
	return wsConn, err
}
