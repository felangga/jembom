package telnet

import (
	"log"
	"net"

	"github.com/felangga/bbman/internal/lobby"
)

type Server struct {
	addr string
	lob  *lobby.Lobby
}

func NewServer(addr string, lob *lobby.Lobby) *Server {
	return &Server{addr: addr, lob: lob}
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
		go newSession(conn, s.lob).run()
	}
}
