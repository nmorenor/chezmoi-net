package net

import (
	"crypto/sha1"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

type KCPSocketHandler struct {
	conn *KCPSocket
}

func (c KCPSocketHandler) Close() error {
	c.conn.Close()
	return nil
}

func (c KCPSocketHandler) Write(p []byte) error {
	c.conn.SendBinary(p)
	return nil
}

type KCPSocket struct {
	conn          *kcp.UDPSession
	Address       string
	handler       *SocketHandler
	isConnected   bool
	key           string
	Timeout       time.Duration
	clientWrapper *KCPSocketHandler
	sendMu        *sync.Mutex // Prevent "concurrent write on connection"
	receiveMu     *sync.Mutex
}

func NewKCPSocket(address string, key string) ISocket {
	socket := &KCPSocket{
		Address:     address,
		handler:     &SocketHandler{},
		Timeout:     0,
		isConnected: false,
		key:         key,
		sendMu:      &sync.Mutex{},
		receiveMu:   &sync.Mutex{},
	}
	socket.clientWrapper = &KCPSocketHandler{socket}
	return socket
}

func (socket *KCPSocket) ClientHandler() ClientHandler {
	return socket.clientWrapper
}

func (socket *KCPSocket) SocketHandler() *SocketHandler {
	return socket.handler
}

func (socket *KCPSocket) Connect() {
	key := pbkdf2.Key([]byte(socket.key), []byte(socket.key), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)

	// dial to the echo server
	if sess, err := kcp.DialWithOptions(socket.Address, block, 10, 3); err == nil {
		socket.conn = sess

		data := time.Now().String()
		_, err = sess.Write([]byte(data))
		if err != nil {
			logger.Error.Println("Error sending data:", err)
		}

		logger.Info.Println("Connected to server")

		if socket.handler.OnConnected != nil {
			socket.isConnected = true
			socket.handler.OnConnected(socket)
		}
		go socket.eventLoop()
	} else {
		socket.isConnected = false
		if socket.handler.OnConnectError != nil {
			socket.handler.OnConnectError(err, socket)
		}
	}

}

func (socket *KCPSocket) Close() {
	socket.conn.Close()
	if socket.handler.OnDisconnected != nil {
		socket.isConnected = false
		socket.handler.OnDisconnected(nil, socket)
	}
}

func (socket *KCPSocket) SendBinary(data []byte) {
	socket.sendMu.Lock()
	defer socket.sendMu.Unlock()
	_, err := socket.conn.Write(data)
	if err != nil {
		if socket.handler.OnDisconnected != nil {
			socket.isConnected = false
			socket.handler.OnDisconnected(err, socket)
		}
		return
	}
}

func (socket *KCPSocket) IsConnected() bool {
	return socket.isConnected
}

func (socket *KCPSocket) eventLoop() {
	for {
		data := make([]byte, 150000)
		n, err := socket.conn.Read(data)
		if err != nil {
			if socket.handler.OnDisconnected != nil {
				socket.isConnected = false
				socket.handler.OnDisconnected(err, socket)
			}
			return
		}
		if socket.handler.OnBinaryMessage != nil {
			socket.handler.OnBinaryMessage(data[:n], socket)
		}
	}
}
