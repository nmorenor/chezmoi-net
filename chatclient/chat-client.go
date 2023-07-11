package chatclient

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nmorenor/chezmoi-net/client"
)

func NewChatClient(currentClient *client.Client, hostMode bool) *ChatClient {
	chatClient := &ChatClient{Client: currentClient, participants: make(map[string]*string), Host: hostMode}
	chatClient.Client.OnConnect = chatClient.onReady
	chatClient.Client.OnSessionChange = chatClient.onSessionChange
	return chatClient
}

type Message struct {
	Source  string
	Message string
}

type ChatClient struct {
	Host         bool
	Client       *client.Client
	participants map[string]*string
}

// This will be called when web socket is connected
func (chatClient *ChatClient) onReady() {
	// Register this (ChatClient) instance to receive rcp calls
	client.RegisterService(chatClient, chatClient.Client)
	var username string
	fmt.Println("username:")
	fmt.Scan(&username)

	if chatClient.Host {
		msg := chatClient.Client.StartHosting(username)
		fmt.Println(msg)
		fmt.Println("Session: " + *chatClient.Client.Session)
	} else {
		var session string
		fmt.Println("session:")
		fmt.Scan(&session)

		msg := chatClient.Client.JoinSession(username, session)
		fmt.Println(msg)
		fmt.Println("Session: " + *chatClient.Client.Session)
	}

	response := chatClient.Client.SessionMembers()
	chatClient.participants = response.Members

	scanner := bufio.NewScanner(os.Stdin)
	for {
		scanner.Scan()
		message := scanner.Text()

		go chatClient.sendMessage(message)
	}
}

func (chatClient *ChatClient) sendMessage(message string) {
	rpcClient := chatClient.Client.GetRpcClientForService(*chatClient)

	// if message starts with [memberName] try to lookup as target
	target := "-1"
	if strings.Index(message, "[") == 0 {
		suffix := message[1:]
		if strings.Contains(suffix, "]") {
			candidate := chatClient.findParticipantFromName(suffix[0:strings.Index(suffix, "]")])
			if candidate != nil {
				message = suffix[strings.Index(suffix, "]")+1:]
				target = *candidate
			}
		}
	}
	sname := chatClient.Client.GetServiceName(*chatClient, "OnMessage", &target)
	if rpcClient != nil {
		var reply string
		rpcClient.Call(sname, Message{Source: *chatClient.Client.Id, Message: message}, &reply)
	}
}

func ptr[T any](t T) *T {
	return &t
}

func (chatClient *ChatClient) findParticipantFromName(target string) *string {
	for id, name := range chatClient.participants {
		if *name == target {
			return &id
		}
	}
	return nil
}

/**
 * Message received from rcp call, RPC methods must follow the signature
 */
func (chatClient *ChatClient) OnMessage(message *Message, reply *string) error {
	if chatClient.participants[message.Source] != nil {
		from := chatClient.participants[message.Source]
		fmt.Printf("%s: %s\n", *from, message.Message)
	}
	*reply = "OK"
	return nil
}

func (chatClient *ChatClient) onSessionChange(event client.SessionChangeEvent) {
	response := chatClient.Client.SessionMembers()
	oldParticipants := chatClient.participants
	chatClient.participants = response.Members
	if event.EventType == client.SESSION_JOIN && chatClient.participants[event.EventSource] != nil {
		fmt.Printf("%s has joined the session\n", *chatClient.participants[event.EventSource])
	}
	if event.EventType == client.SESSION_LEAVE && oldParticipants[event.EventSource] != nil {
		fmt.Printf("%s has leaved the session\n", *oldParticipants[event.EventSource])
	}
	if event.EventType == client.SESSION_END {
		fmt.Println("Session closed")
		chatClient.Client.Close()
	}
}
