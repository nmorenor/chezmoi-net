package hub

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maniartech/signals"

	"encoding/json"

	"github.com/nmorenor/chezmoi-net/jsonrpc"
	cnet "github.com/nmorenor/chezmoi-net/net"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	Hub     *Hub
	Session *string
	Id      string
	Name    *string
	mutex   *sync.Mutex

	Socket    *Socket
	Events    map[string]*signals.Signal[[]byte]
	terminate *signals.Signal[string]
	rpcClient *rpc.Client
	ctx       *context.Context
	Ready     bool
}

func (c *Client) startJsonRPC() {
	// setup communication to client
	clientCon := cnet.NetConn(c.ctx, *c.Events["Client"], nil, c.terminate, c.Socket.Conn)
	c.rpcClient = jsonrpc.NewClient(clientCon) // jsonrpc2 client
	c.Hub.Register <- c

	// hub has registered the client
	sessionManagerConn := cnet.NetConn(c.ctx, *c.Events["SessionManager"], nil, c.terminate, c.Socket.Conn)
	hubConn := cnet.NetConn(c.ctx, *c.Events["Hub"], nil, c.terminate, c.Socket.Conn)
	go jsonrpc.ServeConn(sessionManagerConn) // jsonrpc2 server
	go jsonrpc.ServeConn(hubConn)            // jsonrpc2 server
}

func ptr[T any](t T) *T {
	return &t
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	sendMu := new(sync.Mutex)
	socket := NewSocket(sendMu, *conn)
	ctx := context.Background()
	clientId := uuid.Must(uuid.NewRandom()).String()
	client := &Client{
		Hub:       hub,
		Socket:    &socket,
		Events:    make(map[string]*signals.Signal[[]byte]),
		terminate: ptr(signals.New[string]()),
		Id:        clientId,
		Ready:     false,
		ctx:       &ctx,
		mutex:     sendMu,
	}
	client.Events["SessionManager"] = ptr(signals.New[[]byte]())
	client.Events["Hub"] = ptr(signals.New[[]byte]())
	client.Events["Client"] = ptr(signals.New[[]byte]())
	client.Socket.OnDisconnected = func(err error, socket Socket) {
		sendMu.Lock()
		defer sendMu.Unlock()
		client.Hub.Unregister <- client
	}
	client.Socket.OnPingReceived = func(data string, socket Socket) {
		sendMu.Lock()
		defer sendMu.Unlock()
		socket.Conn.WriteMessage(websocket.PongMessage, []byte("nice"))
	}
	client.Socket.OnTextMessage = func(message string, socket Socket) {
		data := make(map[string]*string)
		json.Unmarshal([]byte(message), &data)
		fmt.Println(message)
	}
	client.Socket.OnBinaryMessage = func(data []byte, socket Socket) {
		mdata := make(map[string]*string)
		json.Unmarshal(data, &mdata)
		if mdata["method"] != nil {
			methodData := *mdata["method"]
			index := strings.Index(methodData, ".")
			channel := methodData
			if index > 0 {
				channel = methodData[0:index]
			}
			if client.Events[channel] != nil {
				evt := *client.Events[channel]
				if evt != nil {
					evt.Emit(*client.ctx, data)
				}
			} else {
				log.Println("1: Channel not found: " + channel)
			}
		} else if mdata["channel"] != nil {
			channel := *mdata["channel"]
			if client.Events[channel] != nil {
				evt := *client.Events[channel]
				if evt != nil {
					evt.Emit(*client.ctx, data)
				}
			} else {
				log.Println("2: Channel not found: " + channel)
			}
		}
	}
	client.Socket.Connect()

	go client.startJsonRPC()
}

func (c *Client) Close() {
	if c.terminate != nil {
		(*c.terminate).Emit(*c.ctx, cnet.KILL)
		(*c.terminate).RemoveListener(cnet.KILL)
	}
}
