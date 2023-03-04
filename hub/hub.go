package hub

import (
	"fmt"
	"log"
	"net/rpc"
	"sync"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	Clients        map[*Client]bool
	IdClients      map[string]*Client
	SessionManager *SessionManager

	// Register requests from the clients.
	Register chan *Client

	// Unregister requests from clients.
	Unregister chan *Client
	mutex      *sync.Mutex
}

func NewHub() *Hub {
	hub := &Hub{
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Clients:    make(map[*Client]bool),
		IdClients:  make(map[string]*Client),
		mutex:      &sync.Mutex{},
	}
	NewSessionManager(hub)
	return hub
}

type SessionMessage struct {
	Session string
	Sender  string
	Channel string
	Target  *string
	Message string
}

func (h *Hub) Handle(message *SessionMessage, reply *string) error {
	if message == nil {
		log.Println("Invalid message")
		return fmt.Errorf("invalid message")
	}
	candidates := []*Client{}
	sess := h.SessionManager.Sessions[message.Session]
	if sess == nil {
		log.Println("Session not found")
		return fmt.Errorf("Session not found")
	}
	for client := range sess.Participants {
		if client.Ready && client.Session != nil && *client.Session == message.Session {
			candidates = append(candidates, client)
		}
	}
	candidates = append(candidates, sess.Host)
	privateMessage := message.Target != nil
	for _, client := range candidates {
		if !privateMessage && client.Id != message.Sender {
			client.rpcClient.Call("Client.Handle", *message, nil)
			continue
		}
		if privateMessage && client.Id == *message.Target {
			var messageReply string
			client.rpcClient.Call("Client.Handle", *message, &messageReply)
			*reply = messageReply
		}
	}
	return nil
}

type RegisteredMessage struct {
	Id string
}

type LeavingMessage struct {
	Id string
}

type ClosedMessage struct {
	Session string
}

func (h *Hub) Run() {
	rpc.Register(h)
	rpc.Register(h.SessionManager)
	for {
		select {
		case client := <-h.Register:
			func() {
				h.mutex.Lock()
				defer h.mutex.Unlock()
				h.Clients[client] = true
				h.IdClients[client.Id] = client
				// send client id back to client
				go func() {
					var status string
					client.rpcClient.Call("Client.Registered", &RegisteredMessage{Id: client.Id}, &status)
				}()
			}()
		case client := <-h.Unregister:
			func() {
				h.mutex.Lock()
				defer h.mutex.Unlock()
				if _, ok := h.Clients[client]; ok {
					if client.Session != nil && h.SessionManager.Sessions[*client.Session] != nil {
						if h.SessionManager.Sessions[*client.Session].Host == client {
							h.SessionManager.CloseSession(*client.Session)
						} else {
							h.SessionManager.LeaveSession(client.Id, *client.Session)
						}
					}

					client.Close() // terminate any json rpc serve routines
					delete(h.Clients, client)
					delete(h.IdClients, client.Id)
				}
			}()
		}
	}
}
