package hub

import (
	"fmt"
)

type SessionManager struct {
	h        *Hub
	Sessions map[string]*Session
}

func NewSessionManager(h *Hub) *SessionManager {
	manager := &SessionManager{h: h, Sessions: make(map[string]*Session)}
	h.SessionManager = manager
	return manager
}

type HostJoinMessage struct {
	Id      string
	Name    string
	Session string
}

func (sessionManager *SessionManager) Host(message *HostJoinMessage, reply *string) error {
	client := sessionManager.h.IdClients[message.Id]
	session := NewSession(message.Session, client)
	client.Name = &message.Name
	client.Session = &message.Session
	client.Ready = true
	sessionManager.Sessions[message.Session] = session
	sessionManager.h.SessionManager.Sessions[message.Session] = session
	*reply = "Welcome to the party " + *client.Name
	return nil
}

func (sessionManager *SessionManager) Join(message *HostJoinMessage, reply *string) error {
	client := sessionManager.h.IdClients[message.Id]
	session := sessionManager.Sessions[message.Session]
	if session == nil {
		*reply = "Session does not exist"
		return fmt.Errorf("Session not found")
	}
	session.Participants[client] = true
	client.Name = &message.Name
	client.Session = &message.Session
	client.Ready = true
	*reply = "Welcome to the party " + *client.Name

	remoteCall := func(c *Client) {
		var status string
		c.rpcClient.Call("Client.MemberJoin", &RegisteredMessage{Id: client.Id}, &status)
	}
	go func(id string, session *Session) {
		for next := range session.Participants {
			if next.Id == id {
				continue
			}
			go remoteCall(next)
		}
		if session.Host.Id != id {
			go remoteCall(session.Host)
		}
	}(client.Id, session)

	return nil
}

func (sessionManager *SessionManager) LeaveSession(targetParticipantId string, targetSession string) {
	session := sessionManager.Sessions[targetSession]
	if session == nil {
		return
	}
	remoteCall := func(c *Client) {
		var status string
		c.rpcClient.Call("Client.ParticipantLeave", &RegisteredMessage{Id: targetParticipantId}, &status)
	}
	go func(id string, session *Session) {
		for next := range session.Participants {
			if next.Id == id {
				continue
			}
			go remoteCall(next)
		}
		if session.Host.Id != id {
			go remoteCall(session.Host)
		}
	}(targetParticipantId, session)
}

func (sessionManager *SessionManager) CloseSession(targetSession string) {
	if sessionManager.Sessions[targetSession] != nil {
		sess := sessionManager.Sessions[targetSession]
		delete(sessionManager.Sessions, targetSession)
		for next := range sess.Participants {
			if sess.Host.Id == next.Id {
				continue
			}
			var status string
			next.rpcClient.Call("Client.SessionClosed", &ClosedMessage{Session: *next.Session}, &status)
			next.Socket.Conn.Close()
		}
	}
}

type MembersMessage struct {
	Session string
}

type MembersMessageResponse struct {
	Members map[string]*string
	Host    string
}

func (sessionManager *SessionManager) Members(message *MembersMessage, reply *MembersMessageResponse) error {
	session := sessionManager.Sessions[message.Session]
	if session == nil {
		return fmt.Errorf("Session not found")
	}
	result := MembersMessageResponse{Members: make(map[string]*string)}
	for next := range session.Participants {
		result.Members[next.Id] = next.Name
	}
	result.Members[session.Host.Id] = session.Host.Name
	result.Host = session.Host.Id
	*reply = result
	return nil
}

type Session struct {
	Id           string
	Host         *Client
	Participants map[*Client]bool
}

func NewSession(id string, host *Client) *Session {
	return &Session{Id: id, Host: host, Participants: make(map[*Client]bool)}
}
