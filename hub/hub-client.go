package hub

import (
	"context"
	"log"
	"net/http"
	"net/rpc"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maniartech/signals"
	"github.com/xtaci/kcp-go/v5"

	"encoding/json"

	"github.com/nmorenor/chezmoi-net/jsonrpc"
	cnet "github.com/nmorenor/chezmoi-net/net"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // TODO: make this more secure
}

type ClientHandler interface {
	Close() error
	Write(p []byte) error
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	Hub     *Hub
	Session *string
	Id      string
	Name    *string
	mutex   *sync.Mutex

	Socket      ISocket
	Events      map[string]*signals.Signal[[]byte]
	terminate   *signals.Signal[string]
	rpcClient   *rpc.Client
	ctx         *context.Context
	Ready       bool
	wrapper     ClientHandler
	pingTicker  *time.Ticker
	isConnected bool
}

func (c *Client) startJsonRPC() {
	// setup communication to client
	clientCon := cnet.NetConn(c.ctx, c.mutex, *c.Events["Client"], nil, c.terminate, c.wrapper)
	c.rpcClient = jsonrpc.NewClient(clientCon) // jsonrpc2 client
	c.Hub.Register <- c

	// hub has registered the client
	sessionManagerConn := cnet.NetConn(c.ctx, c.mutex, *c.Events["SessionManager"], nil, c.terminate, c.wrapper)
	hubConn := cnet.NetConn(c.ctx, c.mutex, *c.Events["Hub"], nil, c.terminate, c.wrapper)
	go jsonrpc.ServeConn(sessionManagerConn) // jsonrpc2 server
	go jsonrpc.ServeConn(hubConn)            // jsonrpc2 server
	c.setupPing()
}

func (client *Client) setupPing() {
	go func(client *Client, ticker *time.Ticker) {
		for {
			select {
			case <-ticker.C:
				if !client.isConnected {
					return
				}
				client.wrapper.Write([]byte("nice"))
			}
		}
	}(client, client.pingTicker)
}

func (c *Client) Close() {
	if c.terminate != nil {
		(*c.terminate).Emit(*c.ctx, cnet.KILL)
		(*c.terminate).RemoveListener(cnet.KILL)
	}
}

func ptr[T any](t T) *T {
	return &t
}

type AvailableSession struct {
	ID              string `json:"id"`
	SessionHostName string `json:"name"`
	Size            int    `json:"size"`
}

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
}

func HubSessions(hub *Hub, w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "GET" {
		return
	}
	currentSessions := []AvailableSession{}
	for id, next := range hub.SessionManager.Sessions {
		currentSessions = append(currentSessions, AvailableSession{ID: id, SessionHostName: *next.Host.Name, Size: len(next.Participants)})
	}
	json.NewEncoder(w).Encode(currentSessions)
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	sendMu := new(sync.Mutex)
	socket := NewWebSocket(sendMu, *conn)
	ctx := context.Background()
	clientId := uuid.Must(uuid.NewRandom()).String()
	client := &Client{
		Hub:         hub,
		Socket:      socket,
		Events:      make(map[string]*signals.Signal[[]byte]),
		terminate:   ptr(signals.New[string]()),
		Id:          clientId,
		Ready:       false,
		ctx:         &ctx,
		mutex:       sendMu,
		wrapper:     ClientHubWSWrapper{Conn: conn},
		pingTicker:  time.NewTicker(30 * time.Second),
		isConnected: true,
	}
	client.Events["SessionManager"] = ptr(signals.New[[]byte]())
	client.Events["Hub"] = ptr(signals.New[[]byte]())
	client.Events["Client"] = ptr(signals.New[[]byte]())
	client.Socket.SocketHandler().OnDisconnected = func(err error, socket interface{}) {
		sendMu.Lock()
		defer sendMu.Unlock()
		client.isConnected = false
		client.Hub.Unregister <- client
	}
	client.Socket.SocketHandler().OnPingReceived = func(data string, socket interface{}) {
		sendMu.Lock()
		defer sendMu.Unlock()
		socket.(*Socket).SendBinary(websocket.PongMessage, []byte("nice"))
	}
	client.Socket.SocketHandler().OnTextMessage = func(message string, socket interface{}) {
		data := make(map[string]*string)
		json.Unmarshal([]byte(message), &data)
	}
	client.Socket.SocketHandler().OnBinaryMessage = func(data []byte, socket interface{}) {
		mdata := make(map[string]*string)
		if string(data) == "hello" {
			sendMu.Lock()
			defer sendMu.Unlock()
			socket.(*Socket).SendBinary(websocket.PongMessage, []byte("nice"))
			return
		}
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

func ServeUDP(hub *Hub, conn *kcp.UDPSession) {
	sendMu := new(sync.Mutex)
	socket := NewKcpSocket(sendMu, conn)
	ctx := context.Background()
	clientId := uuid.Must(uuid.NewRandom()).String()
	client := &Client{
		Hub:         hub,
		Socket:      socket,
		Events:      make(map[string]*signals.Signal[[]byte]),
		terminate:   ptr(signals.New[string]()),
		Id:          clientId,
		Ready:       false,
		ctx:         &ctx,
		mutex:       sendMu,
		pingTicker:  time.NewTicker(30 * time.Second),
		isConnected: true,
	}
	wrapper := &ClientHubUDPWrapper{conn: conn, LastPing: time.Now(), socket: socket}
	client.wrapper = wrapper
	client.Events["SessionManager"] = ptr(signals.New[[]byte]())
	client.Events["Hub"] = ptr(signals.New[[]byte]())
	client.Events["Client"] = ptr(signals.New[[]byte]())
	client.Socket.SocketHandler().OnDisconnected = func(err error, socket interface{}) {
		sendMu.Lock()
		defer sendMu.Unlock()
		client.isConnected = false
		client.Hub.Unregister <- client
		wrapper.conn = nil
	}
	client.Socket.SocketHandler().OnPingReceived = func(data string, socket interface{}) {
		socket.(ISocket).SendBinary(websocket.PongMessage, []byte("nice"))
	}
	client.Socket.SocketHandler().OnTextMessage = func(message string, socket interface{}) {
		data := make(map[string]*string)
		json.Unmarshal([]byte(message), &data)
	}
	client.Socket.SocketHandler().OnBinaryMessage = func(data []byte, socket interface{}) {
		mdata := make(map[string]*string)
		if string(data) == "hello" {
			wrapper.LastPing = time.Now()
			socket.(ISocket).SendBinary(websocket.PongMessage, []byte("nice"))
			return
		}
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
