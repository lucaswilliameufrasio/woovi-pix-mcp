package main

import (
	"log"
	"net/http"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
)

func main() {
	server := &http.Server{Addr: "127.0.0.1:8081", Handler: charge.NewSimulator()}
	log.Printf("Woovi charge simulator listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
