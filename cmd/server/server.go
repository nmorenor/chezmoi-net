package main

import (
	"crypto/sha1"
	"flag"
	"log"
	"net/http"

	"github.com/nmorenor/chezmoi-net/hub"
	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

var addr = flag.String("addr", ":8080", "http service address")
var kaddr = flag.String("kaddr", "0.0.0.0:1305", "http service address")
var kcpkey = flag.String("kcpkey", "demo", "kcp key")

/**
 * Initialize
 * @method App Server
 * @return
 */
func app() {
	flag.Parse()
	log.Println("Starting...")
	hubInstance := hub.NewHub()
	go hubInstance.Run()
	http.HandleFunc("/hub-sessions", func(w http.ResponseWriter, r *http.Request) {
		hub.HubSessions(hubInstance, w, r)
	})
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(hubInstance, w, r)
	})
	go startKCP(hubInstance)
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe error: ", err)
	}
}

func startKCP(hubInstance *hub.Hub) {
	key := pbkdf2.Key([]byte(*kcpkey), []byte(*kcpkey), 1024, 32, sha1.New)
	block, _ := kcp.NewAESBlockCrypt(key)
	if listener, err := kcp.ListenWithOptions(*kaddr, block, 10, 3); err == nil {
		log.Println("KCP Listen on", listener.Addr())
		for {
			if conn, err := listener.AcceptKCP(); err == nil {
				go hub.ServeUDP(hubInstance, conn)
			} else {
				log.Fatal(err)
			}
		}
	} else {
		log.Fatal(err)
	}
}

func main() {
	app()
}
