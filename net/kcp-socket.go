package net

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"net"
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
		err := SendUDP(socket.conn, []byte("hello"))
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
	if !socket.isConnected {
		return
	}
	socket.conn.Close()
	if socket.handler.OnDisconnected != nil {
		socket.isConnected = false
		socket.handler.OnDisconnected(nil, socket)
	}
}

func (socket *KCPSocket) SendBinary(data []byte) {
	socket.sendMu.Lock()
	defer socket.sendMu.Unlock()
	err := SendUDP(socket.conn, data)
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
		data, err := ReceiveUDP(socket.conn)
		if err != nil {
			if socket.handler.OnDisconnected != nil {
				socket.isConnected = false
				socket.handler.OnDisconnected(err, socket)
			}
			return
		}
		if socket.handler.OnBinaryMessage != nil {
			socket.handler.OnBinaryMessage(data, socket)
		}
	}
}

func ReceiveUDP(conn net.Conn) ([]byte, error) {
	r := bufio.NewReader(conn)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	message := make([]byte, length)
	if _, err := io.ReadFull(r, message); err != nil {
		return nil, err
	}
	return message, nil
}

func SendUDP(conn net.Conn, message []byte) error {
	w := bufio.NewWriter(conn)
	length := uint32(len(message))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return err
	}
	return w.Flush()
}
