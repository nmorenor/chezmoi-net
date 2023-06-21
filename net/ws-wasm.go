//go:build js
// +build js

package net

import (
	"context"
	"crypto/tls"

	"nhooyr.io/websocket"
)

// Note: You cant inject tlsConfig here, you are required to use the tlsConfiguration as defined by the browser.
func dialWs(ctx context.Context, url string, tlsConfig *tls.Config) (*websocket.Conn, error) {
	wsConn, _, err := websocket.Dial(ctx, url, nil)
	wsConn.SetReadLimit(150000)
	return wsConn, err
}
