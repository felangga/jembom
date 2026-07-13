package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/felangga/jembom/internal/db"
	"github.com/felangga/jembom/internal/lobby"
	"github.com/felangga/jembom/internal/telnet"
)

func main() {
	database, err := db.Open("jembom.db")
	if err != nil {
		log.Fatal("open db:", err)
	}
	defer database.Close()

	lob := lobby.New()
	if msgs, err := database.RecentChat(20); err == nil {
		lob.SeedChat(msgs)
	} else {
		log.Printf("warn: could not seed chat: %v", err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "2001"
	}
	addr := ":" + port
	srv := telnet.NewServer(addr, lob, database)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Listen(); err != nil {
			log.Fatal(err)
		}
	}()

	log.Printf("Jembom started — telnet localhost %s", port)
	<-sigCh
	log.Println("shutting down")
}
