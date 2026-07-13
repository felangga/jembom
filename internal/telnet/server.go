package telnet

import (
	"log"
	"net"

	"github.com/felangga/jembom/internal/db"
	"github.com/felangga/jembom/internal/lobby"
)

type Server struct {
	addr string
	lob  *lobby.Lobby
	db   *db.DB
}

func NewServer(addr string, lob *lobby.Lobby, database *db.DB) *Server {
	return &Server{addr: addr, lob: lob, db: database}
}

func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("listening on %s", s.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println("accept:", err)
			continue
		}
		log.Printf("new connection from %s", conn.RemoteAddr())
		go newSession(conn, s.lob, s.db).run()
	}
}
