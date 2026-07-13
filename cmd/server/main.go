package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/felangga/bbman/internal/db"
	"github.com/felangga/bbman/internal/lobby"
	"github.com/felangga/bbman/internal/telnet"
)

func main() {
	database, err := db.Open("bbman.db")
	if err != nil {
		log.Fatal("open db:", err)
	}
	defer database.Close()

	lob := lobby.New()
	srv := telnet.NewServer(":2001", lob, database)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Listen(); err != nil {
			log.Fatal(err)
		}
	}()

	log.Println("BBMan started — telnet localhost 2001")
	<-sigCh
	log.Println("shutting down")
}
