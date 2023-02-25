package hub

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maniartech/signals"
)

const (
	KILL = "kill"
)

func NetConn(ctx *context.Context, mutex *sync.Mutex, event signals.Signal[[]byte], outEvent *signals.Signal[[]byte], terminateSignal *signals.Signal[string], c *websocket.Conn) net.Conn {
	nc := &netConn{
		c:         c,
		ctx:       ctx,
		mutex:     mutex,
		outEvt:    outEvent,
		terminate: terminateSignal,
		inEvt:     &event,
		Incomming: make(chan []byte),
		cleanKey:  uuid.Must(uuid.NewRandom()).String(),
	}

	var cancel context.CancelFunc
	nc.writeContext, cancel = context.WithCancel(context.Background())
	nc.writeTimer = time.AfterFunc(math.MaxInt64, cancel)
	nc.writeTimer.Stop()

	nc.readContext, cancel = context.WithCancel(context.Background())
	nc.readTimer = time.AfterFunc(math.MaxInt64, cancel)
	nc.readTimer.Stop()

	event.AddListener(func(ctx context.Context, b []byte) {
		go func(data []byte) {
			nc.Incomming <- data
		}(b)
	}, nc.cleanKey)

	if terminateSignal != nil {
		(*terminateSignal).AddListener(func(ctx context.Context, s string) {
			go func() {
				nc.Incomming <- []byte(KILL)
			}()
		}, KILL)
	}

	return nc
}

type netConn struct {
	c         *websocket.Conn
	outEvt    *signals.Signal[[]byte]
	inEvt     *signals.Signal[[]byte]
	terminate *signals.Signal[string]
	ctx       *context.Context
	mutex     *sync.Mutex
	Incomming chan []byte
	cleanKey  string

	writeTimer   *time.Timer
	writeContext context.Context

	readTimer   *time.Timer
	readContext context.Context

	reader io.Reader
}

var _ net.Conn = &netConn{}

func (c *netConn) Close() error {
	(*c.inEvt).RemoveListener(c.cleanKey)
	if c.c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.c.WriteMessage(websocket.CloseMessage, []byte{})
	return c.c.Close()
}

func (c *netConn) Write(p []byte) (int, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.c != nil {
		err := c.c.WriteMessage(websocket.BinaryMessage, p)
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	if c.outEvt != nil {
		evt := *c.outEvt
		evt.Emit(*c.ctx, p)
		return len(p), nil
	}

	return 0, fmt.Errorf("invalid target service")
}

func (c *netConn) Read(p []byte) (int, error) {

	if c.reader == nil {
		r := <-c.Incomming
		if string(r) == KILL {
			return 0, fmt.Errorf("Terminated")
		}
		c.reader = bytes.NewReader(r)
	}
	n, err := c.reader.Read(p)
	c.reader = nil
	return n, err
}

type unknownAddr struct {
}

func (a unknownAddr) Network() string {
	return "unknown"
}

func (a unknownAddr) String() string {
	return "unknown"
}

func (c *netConn) RemoteAddr() net.Addr {
	return unknownAddr{}
}

func (c *netConn) LocalAddr() net.Addr {
	return unknownAddr{}
}

func (c *netConn) SetDeadline(t time.Time) error {
	c.SetWriteDeadline(t)
	c.SetReadDeadline(t)
	return nil
}

func (c *netConn) SetWriteDeadline(t time.Time) error {
	c.writeTimer.Reset(t.Sub(time.Now()))
	return nil
}

func (c *netConn) SetReadDeadline(t time.Time) error {
	c.readTimer.Reset(t.Sub(time.Now()))
	return nil
}
