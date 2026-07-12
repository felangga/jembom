package telnet

import (
	"bufio"
	"net"

	"github.com/felangga/bbman/internal/lobby"
	"github.com/felangga/bbman/internal/render"
)

const (
	iacByte = 255
	will    = 251
	wont    = 252
	do      = 253
	dont    = 254
	sb      = 250
	se      = 240
	optEcho = 1
	optSGA  = 3
)

type session struct {
	conn net.Conn
	rd   *bufio.Reader
	lob  *lobby.Lobby
}

func newSession(conn net.Conn, lob *lobby.Lobby) *session {
	return &session{conn: conn, rd: bufio.NewReader(conn), lob: lob}
}

func (s *session) run() {
	defer s.conn.Close()
	s.negotiate()

	// Single input goroutine for the lifetime of the connection.
	inputCh := make(chan byte, 64)
	go s.readLoop(inputCh)

	for {
		name := s.readName(inputCh)
		if name == "" {
			return
		}
		if !s.playRound(name, inputCh) {
			return
		}
	}
}

func (s *session) negotiate() {
	// Ask client for character-at-a-time mode, suppress local echo.
	s.conn.Write([]byte{ //nolint:errcheck
		iacByte, will, optEcho,
		iacByte, will, optSGA,
		iacByte, do, optSGA,
	})
}

func (s *session) write(b []byte) { s.conn.Write(b) } //nolint:errcheck

func (s *session) readLoop(inputCh chan<- byte) {
	for {
		b, err := s.readByte()
		if err != nil {
			close(inputCh)
			return
		}
		inputCh <- b
	}
}

func (s *session) readName(inputCh <-chan byte) string {
	s.write(render.Welcome())
	var name []byte
	for b := range inputCh {
		switch {
		case b == '\r' || b == '\n':
			if len(name) > 0 {
				s.write([]byte("\r\n"))
				return string(name)
			}
		case b == 127 || b == 8:
			if len(name) > 0 {
				name = name[:len(name)-1]
				s.write([]byte("\b \b"))
			}
		case b >= 32 && b <= 126 && len(name) < 12:
			name = append(name, b)
			s.write([]byte{b})
		}
	}
	return "" // connection closed
}

// playRound joins the lobby, plays one game, then returns true (play again) or false (disconnect).
func (s *session) playRound(name string, inputCh <-chan byte) bool {
	readyCh := make(chan struct{})
	doneCh := make(chan struct{})

	w := &lobby.Waiter{
		Name:    name,
		Writer:  s.conn,
		ReadyCh: readyCh,
		DoneCh:  doneCh,
	}

	joinDone := make(chan struct{})
	go func() {
		defer close(joinDone)
		s.lob.Join(w)
	}()

	// Wait until the lobby assigns a game.
	select {
	case <-readyCh:
	case <-joinDone:
		return false // lobby returned without starting (disconnect)
	}

	g := w.Game
	slot := w.Slot

	// Forward input to the game until it ends.
	go func() {
		for {
			select {
			case b, ok := <-inputCh:
				if !ok {
					return
				}
				g.SendInput(slot, b)
			case <-doneCh:
				return
			}
		}
	}()

	// Wait for game + game-over screen.
	<-joinDone

	// Wait for a key before returning to welcome screen.
	select {
	case _, ok := <-inputCh:
		return ok
	}
}

// readByte reads one logical byte, transparently consuming IAC negotiation sequences
// and translating arrow-key escape sequences to synthetic keycodes.
func (s *session) readByte() (byte, error) {
	for {
		b, err := s.rd.ReadByte()
		if err != nil {
			return 0, err
		}

		if b == 27 { // ESC — start of arrow key sequence
			next, err := s.rd.ReadByte()
			if err != nil {
				return 0, err
			}
			if next == '[' {
				arrow, err := s.rd.ReadByte()
				if err != nil {
					return 0, err
				}
				switch arrow {
				case 'A':
					return 0x01, nil // up
				case 'B':
					return 0x02, nil // down
				case 'C':
					return 0x04, nil // right
				case 'D':
					return 0x03, nil // left
				}
			}
			continue
		}

		if b != iacByte {
			return b, nil
		}

		// IAC: read and discard the negotiation sequence.
		cmd, err := s.rd.ReadByte()
		if err != nil {
			return 0, err
		}
		if cmd == sb {
			for {
				c, err := s.rd.ReadByte()
				if err != nil {
					return 0, err
				}
				if c == iacByte {
					s.rd.ReadByte() //nolint:errcheck // SE
					break
				}
			}
			continue
		}
		if cmd == will || cmd == wont || cmd == do || cmd == dont {
			s.rd.ReadByte() //nolint:errcheck
		}
	}
}
