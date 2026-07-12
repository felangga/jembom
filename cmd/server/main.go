package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/felangga/bbman/internal/lobby"
	"github.com/felangga/bbman/internal/telnet"
)

func main() {
	lob := lobby.New()
	srv := telnet.NewServer(":2000", lob)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Listen(); err != nil {
			log.Fatal(err)
		}
	}()

	log.Println("BBMan started — telnet localhost 2323")
	<-sigCh
	log.Println("shutting down")
}
