package hub

import (
	"sync"
	"time"

	"github.com/nmorenor/chezmoi-net/net"
	"github.com/xtaci/kcp-go/v5"
)

type KcpSocket struct {
	conn        *kcp.UDPSession
	handler     *SocketHandler
	isConnected bool
	Timeout     time.Duration
	sendMu      *sync.Mutex // Prevent "concurrent write on connection"
	receiveMu   *sync.Mutex
}

type ClientHubUDPWrapper struct {
	conn     *kcp.UDPSession
	socket   KcpSocket
	LastPing time.Time
}

func (c ClientHubUDPWrapper) Close() error {
	c.socket.Close()
	return nil
}

func (c ClientHubUDPWrapper) Write(p []byte) error {
	c.socket.sendMu.Lock()
	defer c.socket.sendMu.Unlock()
	err := net.SendUDP(c.conn, p)
	if err != nil {
		c.socket.Close()
		return err
	}
	elapsed := time.Since(c.LastPing)
	if elapsed > 60*time.Second {
		c.socket.Close()
		return err
	}
	return nil
}

func NewKcpSocket(mutex *sync.Mutex, conn *kcp.UDPSession) ISocket {
	socket := KcpSocket{
		conn:      conn,
		handler:   &SocketHandler{},
		Timeout:   0,
		sendMu:    mutex,
		receiveMu: &sync.Mutex{},
	}
	return socket
}

func (socket KcpSocket) IsConnected() bool {
	return socket.isConnected
}

func (socket KcpSocket) SocketHandler() *SocketHandler {
	return socket.handler
}

func (socket KcpSocket) Close() {
	if !socket.isConnected {
		return
	}
	socket.conn.Close()
	if socket.handler.OnDisconnected != nil {
		socket.isConnected = false
		socket.handler.OnDisconnected(nil, socket)
	}
}

func (socket KcpSocket) SendBinary(messageType int, data []byte) {
	socket.sendMu.Lock()
	defer socket.sendMu.Unlock()
	err := net.SendUDP(socket.conn, data)
	if err != nil {
		if socket.handler.OnDisconnected != nil {
			socket.isConnected = false
			socket.handler.OnDisconnected(err, socket)
		}
		return
	}
}

func (socket KcpSocket) Connect() {
	go func() {

		for {
			socket.receiveMu.Lock()
			data, err := net.ReceiveUDP(socket.conn)
			socket.receiveMu.Unlock()
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
	}()
}
