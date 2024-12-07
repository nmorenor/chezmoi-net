//go:build !js
// +build !js

package net

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"nhooyr.io/websocket"
)

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
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	})
	if err != nil {
		fmt.Println("Error while connecting to server ", err)
		return nil, err
	}
	wsConn.SetReadLimit(150000)
	return wsConn, err
}
