package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/nmorenor/chezmoi-net/chatclient"
	"github.com/nmorenor/chezmoi-net/client"

	"github.com/sacOO7/gowebsocket"
)

/**
 * Initialize
 * @method app
 * @return
 */
func app(hostMode bool) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	currentClient := client.NewClient(gowebsocket.New("ws://localhost:8080/ws"))
	hostClient := chatclient.NewChatClient(currentClient, hostMode)
	if hostClient.Host {
		fmt.Println("Starting as Host")
	} else {
		fmt.Println("Starting as Participant")
	}
	go func() {
		<-hostClient.Client.Interrupt
		interrupt <- os.Kill
	}()
	currentClient.Connect()
	<-interrupt
	log.Println("Interrupt")
	currentClient.Close()
	log.Println("Exiting")
}

func main() {
	joinMode := flag.Bool("join", false, "Join as participant")
	flag.Parse()
	app(!*joinMode)
}
