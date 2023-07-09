package net

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
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

type SocketHandler struct {
	OnConnected     func(socket interface{})
	OnTextMessage   func(message string, socket interface{})
	OnBinaryMessage func(data []byte, socket interface{})
	OnConnectError  func(err error, socket interface{})
	OnDisconnected  func(err error, socket interface{})
	OnPingReceived  func(data string, socket interface{})
	OnPongReceived  func(data string, socket interface{})
}

type ISocket interface {
	SocketHandler() *SocketHandler
	Connect()
	Close()
	SendBinary(data []byte)
	IsConnected() bool
	ClientHandler() ClientHandler
}

type Socket struct {
	WebSocketConn     *websocket.Conn
	Context           context.Context
	Url               string
	ConnectionOptions ConnectionOptions
	RequestHeader     http.Header
	handler           *SocketHandler
	isConnected       bool
	Timeout           time.Duration
	clientWrapper     *ClientWSWrapper
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

type ClientWSWrapper struct {
	Conn    *websocket.Conn
	Context context.Context
}

func (c ClientWSWrapper) Close() error {
	return c.Conn.Close(websocket.StatusNormalClosure, "")
}

func (c ClientWSWrapper) Write(p []byte) error {
	ctx, function := context.WithTimeout(c.Context, 5*time.Second)
	defer function()
	err := c.Conn.Write(ctx, websocket.MessageBinary, p)
	if err != nil {
		logger.Error.Println(err)
	}
	return err
}

func NewWebSocket(url string) ISocket {
	socket := Socket{
		Url:           url,
		RequestHeader: http.Header{},
		ConnectionOptions: ConnectionOptions{
			UseCompression: false,
			UseSSL:         true,
		},
		Timeout:       0,
		handler:       &SocketHandler{},
		clientWrapper: &ClientWSWrapper{},
	}
	return socket
}

func (socket Socket) ClientHandler() ClientHandler {
	return socket.clientWrapper
}

func (socket Socket) SocketHandler() *SocketHandler {
	return socket.handler
}

func (socket Socket) IsConnected() bool {
	return socket.isConnected
}

func (socket Socket) Connect() {
	var err error
	var resp *http.Response

	socket.WebSocketConn, socket.Context, err = socket.dialWebsocket(socket.Url, &tls.Config{
		InsecureSkipVerify: socket.ConnectionOptions.UseSSL,
	})

	// socket.Conn, resp, err = socket.WebsocketDialer.Dial(socket.Url, socket.RequestHeader)

	if err != nil {
		fmt.Println("Error while connecting to server ", err)
		if resp != nil {
			logger.Error.Println(fmt.Sprintf("HTTP Response %d status: %s", resp.StatusCode, resp.Status))
		}
		socket.isConnected = false
		if socket.handler.OnConnectError != nil {
			socket.handler.OnConnectError(err, socket)
		}
		return
	}

	logger.Info.Println("Connected to server")

	if socket.handler.OnConnected != nil {
		socket.isConnected = true
		socket.handler.OnConnected(socket)
	}
	socket.clientWrapper.Conn = socket.WebSocketConn
	socket.clientWrapper.Context = socket.Context

	go socket.eventLoop()
}

func (socket Socket) dialWebsocket(url string, tlsConfig *tls.Config) (*websocket.Conn, context.Context, error) {
	ctx := context.Background()

	wsConn, err := dialWs(ctx, url, tlsConfig)
	if err != nil {
		return nil, nil, err
	}

	return wsConn, ctx, nil
}

func (socket Socket) SendBinary(data []byte) {
	err := socket.WebSocketConn.Write(context.Background(), websocket.MessageBinary, data)
	if err != nil {
		logger.Error.Println("write:", err)
		return
	}
}

func (socket Socket) Close() {
	if socket.WebSocketConn != nil {
		err := socket.WebSocketConn.Close(websocket.StatusNormalClosure, "")
		if err != nil {
			logger.Error.Println("write close:", err)
		}
	}

	if socket.handler.OnDisconnected != nil {
		socket.isConnected = false
		socket.handler.OnDisconnected(fmt.Errorf("normal close"), socket)
	}
}

func (ws Socket) eventLoop() {
	for {
		mType, r, err := ws.WebSocketConn.Reader(context.Background())
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok {
				// The connection has been closed.
				fmt.Println("Connection closed with status", closeErr.Code, "and reason:", closeErr.Reason)
				if ws.handler.OnDisconnected != nil {
					ws.handler.OnDisconnected(err, ws)
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
			if ws.handler.OnTextMessage != nil {
				ws.handler.OnTextMessage(string(data), ws)
			}

		case websocket.MessageBinary:
			data, err := io.ReadAll(r)
			if err != nil {
				// handle error
				continue
			}
			if ws.handler.OnBinaryMessage != nil {
				ws.handler.OnBinaryMessage(data, ws)
			}
		}
	}
}
