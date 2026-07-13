package telnet

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/felangga/bbman/internal/db"
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
	conn   net.Conn
	rd     *bufio.Reader
	lob    *lobby.Lobby
	db     *db.DB
	mode   render.Mode
	writer io.Writer // conn or CP437Writer wrapping conn
}

func newSession(conn net.Conn, lob *lobby.Lobby, database *db.DB) *session {
	s := &session{conn: conn, rd: bufio.NewReader(conn), lob: lob, db: database}
	s.writer = conn
	return s
}

func (s *session) remoteIP() string {
	addr := s.conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func (s *session) detectMode(inputCh <-chan byte) {
	s.conn.Write([]byte( //nolint:errcheck
		"\r\n\033[1mBBMan - Terminal Setup\033[0m\r\n" +
			"  1. Unicode / UTF-8  (modern terminals)\r\n" +
			"  2. CP437 / DOS      (BBS, DOSBox, old IBM PC)\r\n" +
			"  3. ASCII            (plain text fallback)\r\n" +
			"Choice [1-3, Enter=1]: ",
	))
	for b := range inputCh {
		switch b {
		case '2':
			s.mode = render.ModeCP437
			s.writer = &render.CP437Writer{W: s.conn}
			s.conn.Write([]byte("2\r\n")) //nolint:errcheck
			return
		case '3', 'n', 'N':
			s.mode = render.ModeASCII
			s.conn.Write([]byte("3\r\n")) //nolint:errcheck
			return
		case '1', '\r', '\n', 'y', 'Y':
			s.mode = render.ModeUnicode
			s.conn.Write([]byte("1\r\n")) //nolint:errcheck
			return
		}
	}
}

func (s *session) run() {
	defer s.conn.Close()
	s.negotiate()

	inputCh := make(chan byte, 64)
	go s.readLoop(inputCh)

	s.detectMode(inputCh)

	name := s.readName(inputCh)
	if name == "" {
		return
	}
	if !s.authenticate(name, inputCh) {
		return
	}
	for s.roomListMenu(name, inputCh) {
	}
}

func (s *session) negotiate() {
	s.conn.Write([]byte{ //nolint:errcheck
		iacByte, will, optEcho,
		iacByte, will, optSGA,
		iacByte, do, optSGA,
	})
}

func (s *session) write(b []byte) { s.writer.Write(b) } //nolint:errcheck

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
	s.write(render.Welcome(s.mode == render.ModeASCII))
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
	return ""
}

// authenticate handles registration (new name) or PIN login (existing name).
func (s *session) authenticate(name string, inputCh <-chan byte) bool {
	exists, err := s.db.UserExists(name)
	if err != nil {
		s.write([]byte("\r\n\033[91mDB error.\033[0m\r\n"))
		return false
	}
	if !exists {
		return s.register(name, inputCh)
	}
	return s.login(name, inputCh)
}

func (s *session) register(name string, inputCh <-chan byte) bool {
	s.write(render.PinPrompt(name, "New player! Set a 6-digit PIN:", "", s.mode == render.ModeASCII))
	pin1 := s.readPin(inputCh)
	if len(pin1) != 6 {
		return false
	}
	s.write(render.PinPrompt(name, "Confirm your PIN:", "", s.mode == render.ModeASCII))
	pin2 := s.readPin(inputCh)
	if pin1 != pin2 {
		s.write(render.PinPrompt(name, "Confirm your PIN:", "PINs do not match. Reconnect to try again.", s.mode == render.ModeASCII))
		s.readPin(inputCh) // drain / wait for disconnect
		return false
	}
	if err := s.db.Register(name, pin1); err != nil {
		s.write([]byte("\r\n\033[91mCould not register. Name may already be taken.\033[0m\r\n"))
		s.db.LogAuth(name, s.remoteIP(), "register_fail") //nolint:errcheck
		return false
	}
	s.db.LogAuth(name, s.remoteIP(), "register_ok") //nolint:errcheck
	return true
}

func (s *session) login(name string, inputCh <-chan byte) bool {
	errMsg := ""
	for attempt := 0; attempt < 3; attempt++ {
		s.write(render.PinPrompt(name, "Enter your 6-digit PIN:", errMsg, s.mode == render.ModeASCII))
		pin := s.readPin(inputCh)
		if pin == "" {
			return false // disconnected
		}
		err := s.db.Verify(name, pin)
		if err == nil {
			s.db.LogAuth(name, s.remoteIP(), "login_ok") //nolint:errcheck
			return true
		}
		if errors.Is(err, db.ErrWrongPIN) {
			s.db.LogAuth(name, s.remoteIP(), "login_fail") //nolint:errcheck
			remaining := 2 - attempt
			if remaining > 0 {
				errMsg = "Wrong PIN. " + strconv.Itoa(remaining) + " attempt(s) left."
			}
			continue
		}
		return false // unexpected error
	}
	s.db.LogAuth(name, s.remoteIP(), "login_lockout") //nolint:errcheck
	s.write(render.PinPrompt(name, "Enter your 6-digit PIN:", "Too many failed attempts. Disconnecting.", s.mode == render.ModeASCII))
	return false
}

// readPin reads exactly 6 digits, masking each with '*'. Returns "" on disconnect.
func (s *session) readPin(inputCh <-chan byte) string {
	var pin []byte
	for b := range inputCh {
		switch {
		case b == '\r' || b == '\n':
			if len(pin) == 6 {
				s.write([]byte("\r\n"))
				return string(pin)
			}
		case b == 127 || b == 8:
			if len(pin) > 0 {
				pin = pin[:len(pin)-1]
				s.write([]byte("\b \b"))
			}
		case b >= '0' && b <= '9' && len(pin) < 6:
			pin = append(pin, b)
			s.write([]byte("*"))
		}
	}
	return ""
}

// roomListMenu shows rooms + leaderboard + chat and handles user input.
// Returns true to stay in outer loop, false to disconnect.
func (s *session) roomListMenu(name string, inputCh <-chan byte) bool {
	handle := s.lob.RegisterViewer(name)
	defer s.lob.UnregisterViewer(handle)

	var leaders []render.LeaderEntry
	var digitBuf []byte
	var chatBuf []byte
	chatMode := false
	lr := render.NewLobbyRenderer(s.mode == render.ModeASCII)

	fullRefresh := func() {
		leaders, _ = s.db.TopPlayers(10)
		select {
		case <-handle.UpdateCh:
		default:
		}
		s.write(lr.Refresh(name, s.lob.GetRooms(), leaders, s.lob.GetChat(), chatBuf, chatMode))
	}
	fullRefresh()

	autoRefresh := time.NewTicker(3 * time.Second)
	defer autoRefresh.Stop()

	for {
		select {
		case <-autoRefresh.C:
			fullRefresh()

		case <-handle.UpdateCh:
			s.write(render.LobbyChatUpdate(s.lob.GetChat(), chatBuf, chatMode, s.mode == render.ModeASCII))

		case b, ok := <-inputCh:
			if !ok {
				return false
			}

			if chatMode {
				switch {
				case b == '\r' || b == '\n':
					if len(chatBuf) > 0 {
						msg := string(chatBuf)
						s.lob.SendChat(name, msg)
						s.db.LogChat(name, msg) //nolint:errcheck
						chatBuf = nil
					}
					s.write(render.LobbyChatUpdate(s.lob.GetChat(), nil, true, s.mode == render.ModeASCII))
				case b == 27: // ESC — cancel
					chatBuf = nil
					chatMode = false
					s.write(render.LobbyInputUpdate(nil, false, s.mode == render.ModeASCII))
				case b == 127 || b == 8:
					if len(chatBuf) > 0 {
						chatBuf = chatBuf[:len(chatBuf)-1]
						s.write(render.LobbyInputUpdate(chatBuf, true, s.mode == render.ModeASCII))
					}
				case b >= 32 && b <= 126 && len(chatBuf) < 60:
					chatBuf = append(chatBuf, b)
					s.write(render.LobbyInputUpdate(chatBuf, true, s.mode == render.ModeASCII))
				}
			} else {
				switch {
				case b == 'q' || b == 'Q':
					return false
				case b == 'r' || b == 'R':
					digitBuf = nil
					fullRefresh()
				case b == 't' || b == 'T':
					chatMode = true
					chatBuf = nil
					s.write(render.LobbyInputUpdate(nil, true, s.mode == render.ModeASCII))
				case b == 'c' || b == 'C':
					roomName := s.promptRoomName(inputCh)
					if roomName == "" {
						fullRefresh()
						continue
					}
					ok := s.enterRoom(name, func(w *lobby.Waiter) {
						s.lob.CreateRoom(roomName, w)
					}, inputCh)
					lr.Reset()
					fullRefresh()
					if !ok {
						return false
					}
				case b == '\r' || b == '\n':
					id, err := strconv.Atoi(string(digitBuf))
					digitBuf = nil
					if err != nil {
						s.write(render.LobbyInputUpdate(nil, false, s.mode == render.ModeASCII))
						continue
					}
					ok := s.enterRoom(name, func(w *lobby.Waiter) {
						if !s.lob.JoinRoom(id, w) {
							s.write([]byte(
								"\r\n\033[91mRoom not available — press any key to go back.\033[0m\r\n",
							))
						}
					}, inputCh)
					lr.Reset()
					fullRefresh()
					if !ok {
						return false
					}
				case b == 127 || b == 8:
					if len(digitBuf) > 0 {
						digitBuf = digitBuf[:len(digitBuf)-1]
						s.write(render.LobbyInputUpdate(digitBuf, false, s.mode == render.ModeASCII))
					}
				default:
					if b >= '0' && b <= '9' && len(digitBuf) < 5 {
						digitBuf = append(digitBuf, b)
						s.write(render.LobbyInputUpdate(digitBuf, false, s.mode == render.ModeASCII))
					}
				}
			}
		}
	}
}

// promptRoomName reads a room name from the user.
func (s *session) promptRoomName(inputCh <-chan byte) string {
	s.write(render.RoomNamePrompt("", s.mode == render.ModeASCII))
	var name []byte
	for b := range inputCh {
		switch {
		case b == '\r' || b == '\n':
			if len(name) > 0 {
				s.write([]byte("\r\n"))
				return string(name)
			}
		case b == 27: // ESC — cancel
			return ""
		case b == 127 || b == 8:
			if len(name) > 0 {
				name = name[:len(name)-1]
				s.write([]byte("\b \b"))
			}
		case b >= 32 && b <= 126 && len(name) < 20:
			name = append(name, b)
			s.write([]byte{b})
		}
	}
	return ""
}

// enterRoom runs joinFn in a goroutine, waits for ReadyCh, forwards input,
// then blocks until the game ends. Returns false on disconnect.
func (s *session) enterRoom(name string, joinFn func(*lobby.Waiter), inputCh <-chan byte) bool {
	readyCh := make(chan struct{})
	doneCh := make(chan struct{})
	w := &lobby.Waiter{
		Name:    name,
		Writer:  s.writer,
		ReadyCh: readyCh,
		DoneCh:  doneCh,
		ASCII:   s.mode == render.ModeASCII,
	}

	joinDone := make(chan struct{})
	go func() {
		defer close(joinDone)
		joinFn(w)
	}()

	select {
	case <-readyCh:
		// game is live
	case <-joinDone:
		// joinFn returned without closing ReadyCh (room full/gone)
		select {
		case _, ok := <-inputCh:
			return ok
		}
	}

	g := w.Game
	slot := w.Slot

	exitCh := make(chan struct{})
	go func() { // input forwarder
		for {
			select {
			case b, ok := <-inputCh:
				if !ok {
					return
				}
				g.SendInput(slot, b)
			case <-doneCh:
				return
			case <-exitCh:
				return
			}
		}
	}()

	deadCh := make(chan struct{})
	go func() { // death watcher
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if !g.IsAlive(slot) {
					close(deadCh)
					return
				}
			case <-doneCh:
				return
			}
		}
	}()

	select {
	case <-deadCh:
		close(exitCh)
		g.SetWriter(slot, io.Discard)
		w.Room.LeaveEarly(w) // remove from room; prevents GameOver write to conn
		s.db.RecordGame(name)                                    //nolint:errcheck
		s.db.RecordWallDestroyed(name, g.BlocksDestroyed[slot]) //nolint:errcheck
		s.write(render.YouDied(s.mode == render.ModeASCII))
		for {
			b, ok := <-inputCh
			if !ok {
				return false
			}
			if b == 27 {
				return true
			}
		}

	case <-joinDone:
		close(exitCh)
		if w.Game != nil {
			if g.Winner == slot {
				s.db.RecordWin(name) //nolint:errcheck
			} else {
				s.db.RecordGame(name) //nolint:errcheck
			}
			s.db.RecordWallDestroyed(name, g.BlocksDestroyed[slot]) //nolint:errcheck
		}
	}

	// GameOver screen written by room.go is now on the connection.
	// Wait for ESC to return to lobby.
	for {
		b, ok := <-inputCh
		if !ok {
			return false
		}
		if b == 27 {
			return true
		}
	}
}

func (s *session) readByte() (byte, error) {
	for {
		b, err := s.rd.ReadByte()
		if err != nil {
			return 0, err
		}

		if b == 27 {
			// Short deadline distinguishes bare ESC from escape sequences.
			s.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)) //nolint:errcheck
			next, err := s.rd.ReadByte()
			s.conn.SetReadDeadline(time.Time{}) //nolint:errcheck
			if err != nil {
				// Timeout → bare ESC key.
				return 27, nil
			}
			if next != '[' {
				s.rd.UnreadByte() //nolint:errcheck
				return 27, nil
			}
			s.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)) //nolint:errcheck
			arrow, err := s.rd.ReadByte()
			s.conn.SetReadDeadline(time.Time{}) //nolint:errcheck
			if err != nil {
				return 27, nil
			}
			switch arrow {
			case 'A':
				return 0x01, nil
			case 'B':
				return 0x02, nil
			case 'C':
				return 0x04, nil
			case 'D':
				return 0x03, nil
			default:
				s.rd.UnreadByte() //nolint:errcheck
				return 27, nil
			}
		}

		if b != iacByte {
			return b, nil
		}

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
					s.rd.ReadByte() //nolint:errcheck
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
