package net

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	logging "github.com/sacOO7/go-logger"
	"nhooyr.io/websocket"
)

type MessageEvent struct {
	Message interface{}
}

type BinaryEvent struct {
	Data []byte
}

type PingEvent struct{}

type Empty struct {
}

var logger = logging.GetLogger(reflect.TypeOf(Empty{}).PkgPath()).SetLevel(logging.OFF)

func (socket Socket) EnableLogging() {
	logger.SetLevel(logging.TRACE)
}

type Socket struct {
	WebSocketConn *websocket.Conn
	Context       context.Context
	// Conn          *net.Conn
	// WebsocketDialer   *Dialer
	Url               string
	ConnectionOptions ConnectionOptions
	RequestHeader     http.Header
	OnConnected       func(socket Socket)
	OnTextMessage     func(message string, socket Socket)
	OnBinaryMessage   func(data []byte, socket Socket)
	OnConnectError    func(err error, socket Socket)
	OnDisconnected    func(err error, socket Socket)
	OnPingReceived    func(data string, socket Socket)
	OnPongReceived    func(data string, socket Socket)
	IsConnected       bool
	Timeout           time.Duration
	sendMu            *sync.Mutex // Prevent "concurrent write to websocket connection"
	receiveMu         *sync.Mutex
}

type ConnectionOptions struct {
	UseCompression bool
	UseSSL         bool
	Proxy          func(*http.Request) (*url.URL, error)
	Subprotocols   []string
}

// todo Yet to be done
type ReconnectionOptions struct {
}

func New(url string) Socket {
	return Socket{
		Url:           url,
		RequestHeader: http.Header{},
		ConnectionOptions: ConnectionOptions{
			UseCompression: false,
			UseSSL:         true,
		},
		// WebsocketDialer: &websocket.Dialer{},
		Timeout:   0,
		sendMu:    &sync.Mutex{},
		receiveMu: &sync.Mutex{},
	}
}

func (socket *Socket) Connect() {
	var err error
	var resp *http.Response

	socket.WebSocketConn, socket.Context, err = socket.dialWebsocket(socket.Url, &tls.Config{
		InsecureSkipVerify: socket.ConnectionOptions.UseSSL,
	})

	// socket.Conn, resp, err = socket.WebsocketDialer.Dial(socket.Url, socket.RequestHeader)

	if err != nil {
		fmt.Println("Error while connecting to server ", err)
		if resp != nil {
			logger.Error.Println("HTTP Response %d status: %s", resp.StatusCode, resp.Status)
		}
		socket.IsConnected = false
		if socket.OnConnectError != nil {
			socket.OnConnectError(err, *socket)
		}
		return
	}

	logger.Info.Println("Connected to server")

	if socket.OnConnected != nil {
		socket.IsConnected = true
		socket.OnConnected(*socket)
	}

	go socket.eventLoop()
}

func (socket *Socket) dialWebsocket(url string, tlsConfig *tls.Config) (*websocket.Conn, context.Context, error) {
	ctx := context.Background()

	wsConn, err := dialWs(ctx, url, tlsConfig)
	if err != nil {
		return nil, nil, err
	}

	return wsConn, ctx, nil
}

func (socket *Socket) SendBinary(data []byte) {
	err := socket.WebSocketConn.Write(context.Background(), websocket.MessageBinary, data)
	if err != nil {
		logger.Error.Println("write:", err)
		return
	}
}

func (socket *Socket) Close() {
	err := socket.WebSocketConn.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		logger.Error.Println("write close:", err)
	}
	if socket.OnDisconnected != nil {
		socket.IsConnected = false
		socket.OnDisconnected(err, *socket)
	}
}

func (ws *Socket) eventLoop() {
	for {
		mType, r, err := ws.WebSocketConn.Reader(context.Background())
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok {
				// The connection has been closed.
				fmt.Println("Connection closed with status", closeErr.Code, "and reason:", closeErr.Reason)
				if ws.OnDisconnected != nil {
					ws.OnDisconnected(err, *ws)
				}
			} else {
				// Some other error occurred.
				fmt.Println("Error reading from WebSocket:", err)
			}
			return
		}

		switch mType {
		case websocket.MessageText:
			data, err := io.ReadAll(r)
			if err != nil {
				// handle error
				continue
			}
			if ws.OnTextMessage != nil {
				ws.OnTextMessage(string(data), *ws)
			}

		case websocket.MessageBinary:
			data, err := io.ReadAll(r)
			if err != nil {
				// handle error
				continue
			}
			if ws.OnBinaryMessage != nil {
				ws.OnBinaryMessage(data, *ws)
			}
		}
	}
}
