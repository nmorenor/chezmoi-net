package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/nmorenor/chezmoi-net/hub"
)

var addr = flag.String("addr", ":8080", "http service address")

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
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(hubInstance, w, r)
	})
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func main() {
	app()
}
