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

type Socket struct {
	Conn            *websocket.Conn
	OnConnected     func(socket Socket)
	OnTextMessage   func(message string, socket Socket)
	OnBinaryMessage func(data []byte, socket Socket)
	OnConnectError  func(err error, socket Socket)
	OnDisconnected  func(err error, socket Socket)
	OnPingReceived  func(data string, socket Socket)
	OnPongReceived  func(data string, socket Socket)
	IsConnected     bool
	Timeout         time.Duration
	sendMu          *sync.Mutex // Prevent "concurrent write to websocket connection"
	receiveMu       *sync.Mutex
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

func NewSocket(mutex *sync.Mutex, conn websocket.Conn) Socket {
	socket := Socket{
		Conn:      &conn,
		Timeout:   0,
		sendMu:    mutex,
		receiveMu: &sync.Mutex{},
	}
	return socket
}

func (socket *Socket) Connect() {
	defaultPingHandler := socket.Conn.PingHandler()
	socket.Conn.SetPingHandler(func(appData string) error {
		logger.Trace.Println("Received PING from server")
		if socket.OnPingReceived != nil {
			socket.OnPingReceived(appData, *socket)
		}
		return defaultPingHandler(appData)
	})

	defaultPongHandler := socket.Conn.PongHandler()
	socket.Conn.SetPongHandler(func(appData string) error {
		logger.Trace.Println("Received PONG from server")
		if socket.OnPongReceived != nil {
			socket.OnPongReceived(appData, *socket)
		}
		return defaultPongHandler(appData)
	})

	defaultCloseHandler := socket.Conn.CloseHandler()
	socket.Conn.SetCloseHandler(func(code int, text string) error {
		result := defaultCloseHandler(code, text)
		logger.Warning.Println("Disconnected from server ", result)
		if socket.OnDisconnected != nil {
			socket.IsConnected = false
			socket.OnDisconnected(errors.New(text), *socket)
		}
		return result
	})

	go func() {
		for {
			socket.receiveMu.Lock()
			if socket.Timeout != 0 {
				socket.Conn.SetReadDeadline(time.Now().Add(socket.Timeout))
			}
			messageType, message, err := socket.Conn.ReadMessage()
			socket.receiveMu.Unlock()
			if err != nil {
				logger.Error.Println("read:", err)
				if socket.OnDisconnected != nil {
					socket.IsConnected = false
					socket.OnDisconnected(err, *socket)
				}
				return
			}
			logger.Info.Printf("recv: %s\n", message)

			switch messageType {
			case websocket.TextMessage:
				if socket.OnTextMessage != nil {
					socket.OnTextMessage(string(message), *socket)
				}
			case websocket.BinaryMessage:
				if socket.OnBinaryMessage != nil {
					socket.OnBinaryMessage(message, *socket)
				}
			}
		}
	}()
}

func (socket *Socket) SendText(message string) {
	err := socket.send(websocket.TextMessage, []byte(message))
	if err != nil {
		logger.Error.Println("write:", err)
		return
	}
}

func (socket *Socket) SendBinary(data []byte) {
	err := socket.send(websocket.BinaryMessage, data)
	if err != nil {
		logger.Error.Println("write:", err)
		return
	}
}

func (socket *Socket) send(messageType int, data []byte) error {
	socket.sendMu.Lock()
	err := socket.Conn.WriteMessage(messageType, data)
	socket.sendMu.Unlock()
	return err
}

func (socket *Socket) Close() {
	err := socket.send(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		logger.Error.Println("write close:", err)
	}
	socket.Conn.Close()
	if socket.OnDisconnected != nil {
		socket.IsConnected = false
		socket.OnDisconnected(err, *socket)
	}
}
