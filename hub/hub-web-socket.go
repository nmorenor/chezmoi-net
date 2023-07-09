/**
 * Modfied from https://github.com/sacOO7/gowebsocket
 */
package hub

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	logging "github.com/sacOO7/go-logger"
)

type Empty struct {
}

var logger = logging.GetLogger(reflect.TypeOf(Empty{}).PkgPath()).SetLevel(logging.OFF)

func (socket Socket) EnableLogging() {
	logger.SetLevel(logging.TRACE)
}

func (socket Socket) GetLogger() logging.Logger {
	return logger
}

type ClientHubWSWrapper struct {
	Conn *websocket.Conn
}

func (c ClientHubWSWrapper) Close() error {
	c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
	return c.Conn.Close()
}

func (c ClientHubWSWrapper) Write(p []byte) error {
	return c.Conn.WriteMessage(websocket.BinaryMessage, p)
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
	SendBinary(messageType int, data []byte)
	IsConnected() bool
}

type Socket struct {
	conn        *websocket.Conn
	handler     *SocketHandler
	isConnected bool
	Timeout     time.Duration
	sendMu      *sync.Mutex // Prevent "concurrent write to websocket connection"
	receiveMu   *sync.Mutex
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

func NewWebSocket(mutex *sync.Mutex, conn websocket.Conn) ISocket {
	socket := Socket{
		conn:      &conn,
		handler:   &SocketHandler{},
		Timeout:   0,
		sendMu:    mutex,
		receiveMu: &sync.Mutex{},
	}
	return socket
}

func (socket Socket) IsConnected() bool {
	return socket.isConnected
}

func (socket Socket) SocketHandler() *SocketHandler {
	return socket.handler
}

func (socket Socket) Close() {
	socket.internalClose()
}

func (socket Socket) Connect() {
	defaultPingHandler := socket.conn.PingHandler()
	socket.conn.SetPingHandler(func(appData string) error {
		logger.Trace.Println("Received PING from server")
		if socket.handler.OnPingReceived != nil {
			socket.handler.OnPingReceived(appData, socket)
		}
		return defaultPingHandler(appData)
	})

	defaultPongHandler := socket.conn.PongHandler()
	socket.conn.SetPongHandler(func(appData string) error {
		logger.Trace.Println("Received PONG from server")
		if socket.handler.OnPongReceived != nil {
			socket.handler.OnPongReceived(appData, socket)
		}
		return defaultPongHandler(appData)
	})

	defaultCloseHandler := socket.conn.CloseHandler()
	socket.conn.SetCloseHandler(func(code int, text string) error {
		result := defaultCloseHandler(code, text)
		logger.Warning.Println("Disconnected from server ", result)
		if socket.handler.OnDisconnected != nil {
			socket.isConnected = false
			socket.handler.OnDisconnected(errors.New(text), socket)
		}
		return result
	})

	go func() {
		for {
			socket.receiveMu.Lock()
			if socket.Timeout != 0 {
				socket.conn.SetReadDeadline(time.Now().Add(socket.Timeout))
			}
			messageType, message, err := socket.conn.ReadMessage()
			socket.receiveMu.Unlock()
			if err != nil {
				logger.Error.Println("read:", err)
				if socket.handler.OnDisconnected != nil {
					socket.isConnected = false
					socket.handler.OnDisconnected(err, socket)
				}
				return
			}
			logger.Info.Printf("recv: %s\n", message)

			switch messageType {
			case websocket.TextMessage:
				if socket.handler.OnTextMessage != nil {
					socket.handler.OnTextMessage(string(message), socket)
				}
			case websocket.BinaryMessage:
				if socket.handler.OnBinaryMessage != nil {
					socket.handler.OnBinaryMessage(message, socket)
				}
			}
		}
	}()
}

func (socket Socket) SendBinary(messageType int, data []byte) {
	err := socket.send(messageType, data)
	if err != nil {
		logger.Error.Println("write:", err)
		return
	}
}

func (socket Socket) send(messageType int, data []byte) error {
	socket.sendMu.Lock()
	err := socket.conn.WriteMessage(messageType, data)
	socket.sendMu.Unlock()
	return err
}

func (socket Socket) internalClose() {
	if !socket.isConnected {
		return
	}
	err := socket.send(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		logger.Error.Println("write close:", err)
	}
	socket.conn.Close()
	if socket.handler.OnDisconnected != nil {
		socket.isConnected = false
		socket.handler.OnDisconnected(err, socket)
	}
}
