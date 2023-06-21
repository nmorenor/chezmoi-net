//go:build !js
// +build !js

package net

import (
	"context"
	"crypto/tls"
	"net/http"

	"nhooyr.io/websocket"
)

func dialWs(ctx context.Context, url string, tlsConfig *tls.Config) (*websocket.Conn, error) {
	wsConn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	})
	wsConn.SetReadLimit(150000)
	return wsConn, err
}
