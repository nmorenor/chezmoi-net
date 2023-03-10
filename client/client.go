package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maniartech/signals"
	"github.com/mborders/artifex"
	cnet "github.com/nmorenor/chezmoi-net/net"
	"github.com/sacOO7/gowebsocket"

	"github.com/nmorenor/chezmoi-net/hub"
	"github.com/nmorenor/chezmoi-net/jsonrpc"
)

const (
	SESSION_JOIN  = 3
	SESSION_LEAVE = 4
	SESSION_END   = 5
)

func ptr[T any](t T) *T {
	return &t
}

type SessionChangeEvent struct {
	EventType   int
	EventSource string
}

type Client struct {
	Id                      *string
	Session                 *string
	Host                    bool
	OnSessionChange         func(event SessionChangeEvent)
	ws                      *gowebsocket.Socket
	netCon                  *net.Conn
	incommingCon            *net.Conn
	hubConn                 *net.Conn
	Interrupt               chan int
	ready                   chan bool
	ctx                     context.Context
	events                  map[string]*signals.Signal[[]byte]
	mainEvents              map[string]*signals.Signal[[]byte]
	servicesInEvents        map[string]*signals.Signal[[]byte]
	servicesOutEvents       map[string]*signals.Signal[[]byte]
	outEvents               map[string]*signals.Signal[[]byte]
	terminate               *signals.Signal[string]
	OnConnect               func()
	sessionManagerRpcClient *rpc.Client
	hubRpcClient            *rpc.Client
	mutex                   *sync.Mutex
	conns                   map[string]*net.Conn
	inconns                 map[string]*net.Conn
	Services                map[string]*rpc.Client
	sessionChanged          *signals.Signal[SessionChangeEvent]
	pingTimer               *time.Timer
	outgoingDispatcher      *artifex.Dispatcher
	incommingDispatcher     *artifex.Dispatcher
}

var IncommingMessage = signals.New[[]byte]()

func NewClient(socket gowebsocket.Socket) *Client {
	wsClient := Client{
		Id:                  nil,
		ws:                  &socket,
		netCon:              nil,
		Interrupt:           make(chan int),
		ready:               make(chan bool),
		ctx:                 context.Background(),
		incommingCon:        nil,
		hubConn:             nil,
		events:              make(map[string]*signals.Signal[[]byte]),
		mainEvents:          make(map[string]*signals.Signal[[]byte]),
		servicesInEvents:    make(map[string]*signals.Signal[[]byte]),
		servicesOutEvents:   make(map[string]*signals.Signal[[]byte]),
		outEvents:           make(map[string]*signals.Signal[[]byte]),
		mutex:               &sync.Mutex{},
		conns:               make(map[string]*net.Conn),
		inconns:             make(map[string]*net.Conn),
		Services:            make(map[string]*rpc.Client),
		sessionChanged:      ptr(signals.New[SessionChangeEvent]()),
		terminate:           ptr(signals.New[string]()),
		pingTimer:           time.NewTimer(30 * time.Second),
		outgoingDispatcher:  artifex.NewDispatcher(10, 100),
		incommingDispatcher: artifex.NewDispatcher(10, 100),
	}
	wsClient.ws.OnConnected = wsClient.OnConnected
	wsClient.ws.OnConnectError = wsClient.OnConnectError
	wsClient.ws.OnDisconnected = wsClient.OnDisconnected
	wsClient.ws.OnBinaryMessage = wsClient.OnBinaryMessage
	wsClient.ws.OnTextMessage = wsClient.OnTextMessage
	wsClient.mainEvents["SessionManager"] = ptr(signals.New[[]byte]())
	wsClient.mainEvents["Client"] = ptr(signals.New[[]byte]())
	wsClient.mainEvents["Hub"] = ptr(signals.New[[]byte]())
	rpc.Register(&wsClient)
	wsClient.outgoingDispatcher.Start()
	wsClient.incommingDispatcher.Start()
	return &wsClient
}

func (client *Client) OnConnectError(err error, socket gowebsocket.Socket) {
	log.Fatal("Received connect error - ", err)
	client.clean()
}

func (client *Client) OnConnected(socket gowebsocket.Socket) {
	netConn := cnet.NetConn(&client.ctx, *client.mainEvents["SessionManager"], nil, client.terminate, client.ws.Conn)  // outgoing
	hubnetConn := cnet.NetConn(&client.ctx, *client.mainEvents["Hub"], nil, client.terminate, client.ws.Conn)          // outgoing
	incommingNetConn := cnet.NetConn(&client.ctx, *client.mainEvents["Client"], nil, client.terminate, client.ws.Conn) // incomming
	client.netCon = &netConn
	client.incommingCon = &incommingNetConn
	client.hubConn = &hubnetConn
	(*client.sessionChanged).AddListener(func(ctx context.Context, event SessionChangeEvent) {
		if client.OnSessionChange != nil {
			client.OnSessionChange(event)
		}
	}, "sessionChanged")
	// ping
	go func(client *Client, timer *time.Timer) {
		<-timer.C
		if !client.ws.IsConnected {
			return
		}
		client.mutex.Lock()
		defer client.mutex.Unlock()
		client.ws.Conn.WriteMessage(websocket.PingMessage, []byte("hello"))
	}(client, client.pingTimer)
	go jsonrpc.ServeConn(*client.incommingCon)

	// wait for server to send client id
	go func(client *Client) {
		<-client.ready
		client.sessionManagerRpcClient = jsonrpc.NewClient(*client.netCon)
		client.hubRpcClient = jsonrpc.NewClient(*client.hubConn)
		go client.OnConnect()
	}(client)

}

func (client *Client) OnDisconnected(err error, socket gowebsocket.Socket) {
	log.Println("Disconnected from server ")
	client.clean()
	client.Interrupt <- 1
}

func (client *Client) OnTextMessage(message string, socket gowebsocket.Socket) {
	data := make(map[string]*string)
	json.Unmarshal([]byte(message), &data)
	// log.Println("Received message - " + message)
}

func (client *Client) OnBinaryMessage(data []byte, socket gowebsocket.Socket) {
	channel := client.getChannelFromMessage(data)
	if channel != nil && client.events[*channel] != nil {
		evt := *client.events[*channel]
		if evt != nil {
			evt.Emit(client.ctx, data)
		}
		return
	} else if channel != nil && client.mainEvents[*channel] != nil {
		evt := *client.mainEvents[*channel]
		if evt != nil {
			evt.Emit(client.ctx, data)
		}
		return
	} else if channel != nil {
		log.Println("Channel not found: " + *channel)
	}
	// log.Println("Received message - " + string(data))
}

func (client *Client) getChannelFromMessage(data []byte) *string {
	mdata := make(map[string]*string)
	json.Unmarshal(data, &mdata)
	if mdata["method"] != nil {
		methodData := *mdata["method"]
		index := strings.Index(methodData, ".")
		channel := methodData
		if index > 0 {
			channel = methodData[0:index]
		}
		return &channel
	} else if mdata["channel"] != nil {
		channel := *mdata["channel"]
		return &channel
	}
	return nil
}

func (client *Client) isRpcRequest(data []byte) bool {
	mdata := make(map[string]*string)
	json.Unmarshal(data, &mdata)
	return mdata["method"] != nil
}

func (client *Client) Connect() {
	client.ws.Connect()
}

func (client *Client) Close() {
	client.clean()
	go client.ws.Close()
}

func (client *Client) clean() {
	fmt.Println("cleaning")
	if client.outgoingDispatcher != nil {
		client.outgoingDispatcher.Stop()
	}
	if client.incommingDispatcher != nil {
		client.incommingDispatcher.Stop()
	}
	if client.terminate != nil {
		(*client.terminate).Emit(client.ctx, cnet.KILL)
		(*client.terminate).RemoveListener(cnet.KILL)
	}
	client.pingTimer.Stop()
	for outevt := range client.outEvents {
		(*client.outEvents[outevt]).RemoveListener(outevt)
	}
	for evt := range client.servicesInEvents {
		(*client.servicesInEvents[evt]).RemoveListener(evt)
	}
	for evt := range client.servicesOutEvents {
		(*client.servicesOutEvents[evt]).RemoveListener(evt)
	}
	for evt := range client.mainEvents {
		(*client.mainEvents[evt]).RemoveListener(evt)
	}
	for _, conn := range client.conns {
		(*conn).Close()
	}
	(*client.sessionChanged).RemoveListener("sessionChanged")
	fmt.Println("cleaned")
}

func (client *Client) Registered(message *hub.RegisteredMessage, reply *string) error {
	client.Id = &message.Id
	client.ready <- true
	*reply = "OK"
	return nil
}

func (client *Client) MemberJoin(message *hub.RegisteredMessage, reply *string) error {
	(*client.sessionChanged).Emit(client.ctx, SessionChangeEvent{EventSource: message.Id, EventType: SESSION_JOIN})
	*reply = "OK"
	return nil
}

func (client *Client) ParticipantLeave(message *hub.LeavingMessage, reply *string) error {
	(*client.sessionChanged).Emit(client.ctx, SessionChangeEvent{EventSource: message.Id, EventType: SESSION_LEAVE})
	*reply = "OK"
	return nil
}

func (client *Client) SessionClosed(message *hub.ClosedMessage, reply *string) error {
	(*client.sessionChanged).Emit(client.ctx, SessionChangeEvent{EventSource: message.Session, EventType: SESSION_END})
	*reply = "OK"
	return nil
}

func (c *Client) StartHosting(username string) string {
	c.Host = true
	c.Session = ptr(uuid.Must(uuid.NewRandom()).String())
	message := hub.HostJoinMessage{Id: *c.Id, Name: username, Session: *c.Session}
	var response string
	c.sessionManagerRpcClient.Call("SessionManager.Host", message, &response)
	return response
}

func (c *Client) JoinSession(username string, session string) string {
	c.Host = false
	c.Session = &session
	message := hub.HostJoinMessage{Id: *c.Id, Name: username, Session: *c.Session}
	var response string
	c.sessionManagerRpcClient.Call("SessionManager.Join", message, &response)
	return response
}

func (c *Client) SessionMembers() hub.MembersMessageResponse {
	message := hub.MembersMessage{Session: *c.Session}
	var response hub.MembersMessageResponse
	c.sessionManagerRpcClient.Call("SessionManager.Members", message, &response)
	return response
}

func (client *Client) Handle(message *hub.SessionMessage, reply *string) error {
	if message == nil {
		log.Println("Invalid message")
		return fmt.Errorf("invalid message")
	}
	msg := message.Message
	return client.handleMessage(msg, reply)
}

func (client *Client) handleMessage(msg string, reply *string) error {
	errorChannel := make(chan *string)
	client.incommingDispatcher.Dispatch(func() {
		targetMessage, decodeError := base64Decode(msg)
		if decodeError {
			errorChannel <- ptr("invalid target message")
			return
		}
		service := client.getChannelFromMessage([]byte(targetMessage))
		rpcRequset := client.isRpcRequest([]byte(targetMessage))

		if rpcRequset {
			inevt := client.servicesInEvents[*service]
			evt := client.servicesOutEvents[*service]
			if evt == nil || inevt == nil {
				errorChannel <- ptr("invalid target out event")
				return
			}
			req := clientRequest{}
			json.Unmarshal([]byte(targetMessage), &req)
			ready := make(chan []byte)
			evtKey := uuid.Must(uuid.NewRandom()).String()
			(*evt).AddListener(func(ctx context.Context, b []byte) {
				ready <- b
			}, evtKey)
			(*inevt).Emit(client.ctx, []byte(targetMessage))
			targetResponse := <-ready
			(*evt).RemoveListener(evtKey)
			clientResp := &clientResponse{}
			json.Unmarshal(targetResponse, clientResp)
			clientResp.Id = req.Id
			clientResp.Channel = service
			tresp, e := JSONMarshal(clientResp)
			if e == nil {
				*reply = base64Encode(string(tresp))
			}
		} else if service != nil && client.servicesInEvents[*service] != nil {
			inevt := client.servicesInEvents[*service]
			if inevt == nil {
				errorChannel <- ptr("invalid target in event")
				return
			}
			(*inevt).Emit(client.ctx, []byte(targetMessage))
		}
		errorChannel <- nil
	})
	result := <-errorChannel
	if result != nil {
		return fmt.Errorf(*result)
	}
	return nil
}

type clientRequest struct {
	Method  string `json:"method"`
	Channel string `json:"channel"`
	Params  [1]any `json:"params"`
	Id      uint64 `json:"id"`
}

type clientResponse struct {
	Id      uint64           `json:"id"`
	Channel *string          `json:"channel"`
	Result  *json.RawMessage `json:"result"`
	Error   any              `json:"error"`
}

func RegisterService[T any](rcvr *T, client *Client, targetProvider func() *string) {
	val := reflect.ValueOf(rcvr)
	sname := reflect.Indirect(val).Type().Name()
	client.events[sname] = ptr(signals.New[[]byte]())
	client.outEvents[sname] = ptr(signals.New[[]byte]())
	client.servicesInEvents[sname] = ptr(signals.New[[]byte]())
	client.servicesOutEvents[sname] = ptr(signals.New[[]byte]())
	conn := cnet.NetConn(&client.ctx, *client.events[sname], client.outEvents[sname], client.terminate, nil)
	inconn := cnet.NetConn(&client.ctx, *client.servicesInEvents[sname], client.servicesOutEvents[sname], client.terminate, nil)
	client.conns[sname] = &conn
	client.inconns[sname] = &inconn
	rpc.Register(rcvr)
	client.Services[sname] = jsonrpc.NewClient(conn)
	(*client.outEvents[sname]).AddListener(func(ctx context.Context, b []byte) {
		client.outgoingDispatcher.Dispatch(func() {
			req := clientRequest{}
			json.Unmarshal(b, &req)
			message := &hub.SessionMessage{
				Session: *client.Session,
				Sender:  *client.Id,
				Channel: sname,
				Target:  targetProvider(),
				Message: base64Encode(string(b)),
			}
			var response string
			err := client.hubRpcClient.Call("Hub.Handle", message, &response)
			response, _ = base64Decode(response)
			remoteResponse := &clientResponse{}
			json.Unmarshal([]byte(response), remoteResponse)

			clientResp := clientResponse{
				Id:      req.Id,
				Channel: &sname,
				Result:  remoteResponse.Result,
				Error:   err,
			}
			data, err := JSONMarshal(clientResp)
			if err != nil {
				log.Println(err)
				return
			}
			(*client.events[sname]).Emit(client.ctx, data)
		})
	}, sname)
	go rpc.ServeCodec(jsonrpc.NewServerCodec(inconn))
}

func JSONMarshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	err := encoder.Encode(t)
	return buffer.Bytes(), err
}

func (client *Client) GetRpcClientForService(service any) *rpc.Client {
	val := reflect.ValueOf(service)
	sname := reflect.Indirect(val).Type().Name()
	return client.Services[sname]
}

func (client *Client) GetServiceName(service any) string {
	val := reflect.ValueOf(service)
	sname := reflect.Indirect(val).Type().Name()
	return sname
}

func base64Encode(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func base64Decode(str string) (string, bool) {
	data, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return "", true
	}
	return string(data), false
}
