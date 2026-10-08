package telnet

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/felangga/jembom/internal/db"
	"github.com/felangga/jembom/internal/lobby"
	"github.com/felangga/jembom/internal/render"
)

const (
	iacByte  = 255
	will     = 251
	wont     = 252
	do       = 253
	dont     = 254
	sb       = 250
	se       = 240
	optEcho  = 1
	optSGA   = 3
	optNAWS  = 31
	optTTYPE = 24
	subIS    = 0
	subSEND  = 1
)

type session struct {
	conn        net.Conn
	rd          *bufio.Reader
	lob         *lobby.Lobby
	db          *db.DB
	mode        render.Mode
	writer      io.Writer
	connectedAt time.Time
	userID      int64
	mu          sync.Mutex // guards term fields (written by readLoop goroutine)
	termType    string
	termW       int
	termH       int
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
		"\r\n\033[1mJembom - Terminal Setup\033[0m\r\n" +
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
	s.connectedAt = time.Now()
	defer s.logSessionEnd()

	s.negotiate()
	log.Printf("[%s] negotiated", s.remoteIP())

	inputCh := make(chan byte, 64)
	go s.readLoop(inputCh)

	s.detectMode(inputCh)
	log.Printf("[%s] mode=%s", s.remoteIP(), s.modeStr())

	name := s.readName(inputCh)
	log.Printf("[%s] name=%q", s.remoteIP(), name)
	if name == "" {
		return
	}
	userID, ok := s.authenticate(name, inputCh)
	log.Printf("[%s] auth ok=%v userID=%d", s.remoteIP(), ok, userID)
	if !ok {
		return
	}
	s.userID = userID
	for s.roomListMenu(name, inputCh) {
		log.Printf("[%s] roomListMenu loop", s.remoteIP())
	}
	log.Printf("[%s] roomListMenu exited", s.remoteIP())
}

func (s *session) logSessionEnd() {
	s.mu.Lock()
	tt, tw, th := s.termType, s.termW, s.termH
	s.mu.Unlock()
	log.Printf("session end ip=%s userID=%d mode=%s term=%s %dx%d dur=%ds",
		s.remoteIP(), s.userID, s.modeStr(), tt, tw, th,
		int(time.Since(s.connectedAt).Seconds()))
	if err := s.db.LogSession(
		s.connectedAt.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		s.remoteIP(),
		s.userID,
		s.modeStr(),
		tt, tw, th,
		int(time.Since(s.connectedAt).Seconds()),
	); err != nil {
		log.Printf("session_log insert failed: %v", err)
	}
}

func (s *session) modeStr() string {
	switch s.mode {
	case render.ModeCP437:
		return "cp437"
	case render.ModeASCII:
		return "ascii"
	case render.ModeUnicode:
		return "unicode"
	default:
		return ""
	}
}

func (s *session) negotiate() {
	s.conn.Write([]byte{ //nolint:errcheck
		iacByte, will, optEcho,
		iacByte, will, optSGA,
		iacByte, do, optSGA,
		iacByte, do, optNAWS,
		iacByte, do, optTTYPE,
	})
}

func (s *session) handleSubneg(data []byte) {
	if len(data) < 1 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch data[0] {
	case optTTYPE:
		if len(data) >= 3 && data[1] == subIS {
			s.termType = string(data[2:])
		}
	case optNAWS:
		if len(data) == 5 {
			s.termW = int(data[1])<<8 | int(data[2])
			s.termH = int(data[3])<<8 | int(data[4])
		}
	}
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
			if len(name) >= 4 {
				s.write([]byte("\r\n"))
				return string(name)
			}
			if len(name) > 0 {
				s.write(render.WelcomeError("Name must be at least 4 characters.", len(name), s.mode == render.ModeASCII))
			}
		case b == 127 || b == 8:
			if len(name) > 0 {
				name = name[:len(name)-1]
				s.write([]byte("\b \b"))
			}
		case b >= 32 && b <= 126 && len(name) < 8:
			name = append(name, b)
			s.write([]byte{b})
		}
	}
	return ""
}

// authenticate handles registration (new name) or PIN login (existing name).
// Returns (userID, true) on success.
func (s *session) authenticate(name string, inputCh <-chan byte) (int64, bool) {
	// Blocklist check happens before anything else: a banned name is refused
	// whether or not the account exists, so a spam account cannot get back in
	// by reconnecting under the same name.
	ip := s.remoteIP()
	blockedIP, err := s.db.IsIPBanned(ip)
	if err != nil {
		s.write([]byte("\r\n\033[91mDB error.\033[0m\r\n"))
		return 0, false
	}
	if blockedIP {
		s.write([]byte("\r\n\033[91mThis address is blocked.\033[0m\r\n"))
		s.db.LogAuth(0, ip, "ip_banned_reject") //nolint:errcheck
		return 0, false
	}
	banned, err := s.db.IsBanned(name)
	if err != nil {
		s.write([]byte("\r\n\033[91mDB error.\033[0m\r\n"))
		return 0, false
	}
	if banned {
		s.write([]byte("\r\n\033[91mThis name is blocked.\033[0m\r\n"))
		s.db.LogAuth(0, ip, "banned_reject") //nolint:errcheck
		return 0, false
	}
	userID, exists, err := s.db.UserExists(name)
	if err != nil {
		s.write([]byte("\r\n\033[91mDB error.\033[0m\r\n"))
		return 0, false
	}
	if !exists {
		return s.register(name, inputCh)
	}
	return s.login(name, userID, inputCh)
}

func (s *session) register(name string, inputCh <-chan byte) (int64, bool) {
	s.write(render.PinPrompt(name, "New player! Set a 6-digit PIN:", "", s.mode == render.ModeASCII))
	pin1 := s.readPin(inputCh)
	if len(pin1) != 6 {
		return 0, false
	}
	s.write(render.PinPrompt(name, "Confirm your PIN:", "", s.mode == render.ModeASCII))
	pin2 := s.readPin(inputCh)
	if pin1 != pin2 {
		s.write(render.PinPrompt(name, "Confirm your PIN:", "PINs do not match. Reconnect to try again.", s.mode == render.ModeASCII))
		s.readPin(inputCh) // drain / wait for disconnect
		return 0, false
	}
	userID, err := s.db.Register(name, pin1)
	if err != nil {
		s.write([]byte("\r\n\033[91mCould not register. Name may already be taken.\033[0m\r\n"))
		s.db.LogAuth(0, s.remoteIP(), "register_fail") //nolint:errcheck
		return 0, false
	}
	s.db.LogAuth(userID, s.remoteIP(), "register_ok") //nolint:errcheck
	return userID, true
}

func (s *session) login(name string, userID int64, inputCh <-chan byte) (int64, bool) {
	errMsg := ""
	for attempt := 0; attempt < 3; attempt++ {
		s.write(render.PinPrompt(name, "Enter your 6-digit PIN:", errMsg, s.mode == render.ModeASCII))
		pin := s.readPin(inputCh)
		if pin == "" {
			return 0, false // disconnected
		}
		err := s.db.Verify(userID, pin)
		if err == nil {
			s.db.LogAuth(userID, s.remoteIP(), "login_ok") //nolint:errcheck
			return userID, true
		}
		if errors.Is(err, db.ErrWrongPIN) {
			s.db.LogAuth(userID, s.remoteIP(), "login_fail") //nolint:errcheck
			remaining := 2 - attempt
			if remaining > 0 {
				errMsg = "Wrong PIN. " + strconv.Itoa(remaining) + " attempt(s) left."
			}
			continue
		}
		return 0, false // unexpected error
	}
	s.db.LogAuth(userID, s.remoteIP(), "login_lockout") //nolint:errcheck
	s.write(render.PinPrompt(name, "Enter your 6-digit PIN:", "Too many failed attempts. Disconnecting.", s.mode == render.ModeASCII))
	return 0, false
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
	var rooms []render.RoomInfo
	selectedRoom := -1
	var chatBuf []byte
	chatMode := false
	var chatTimes [3]time.Time // ring buffer of last 3 send timestamps
	chatIdx := 0
	lastMsg := ""
	lr := render.NewLobbyRenderer(s.mode == render.ModeASCII)

	fullRefresh := func() {
		leaders, _ = s.db.TopPlayers(10)
		rooms = s.lob.GetRooms()
		if selectedRoom >= len(rooms) {
			selectedRoom = len(rooms) - 1
		}
		select {
		case <-handle.UpdateCh:
		default:
		}
		s.write(lr.Refresh(name, rooms, leaders, s.lob.GetChat(), chatBuf, chatMode, selectedRoom, s.lob.OnlineCount()))
	}
	fullRefresh()

	autoRefresh := time.NewTicker(3 * time.Second)
	defer autoRefresh.Stop()

	for {
		select {
		case <-autoRefresh.C:
			fullRefresh()

		case <-handle.UpdateCh:
			rooms = s.lob.GetRooms()
			if selectedRoom >= len(rooms) {
				selectedRoom = len(rooms) - 1
			}
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
							now := time.Now()
							oldest := chatTimes[chatIdx]
							spammy := !oldest.IsZero() && now.Sub(oldest) < 5*time.Second
							duplicate := msg == lastMsg
							// Blocked words are dropped silently: the message is neither
							// shown nor stored, so a spam run produces no trace in chat.
							// spamHits in a row from one account blocks that address.
							const spamHitsToBan = 3
							if spam, err := s.db.IsSpam(msg); err == nil && spam {
								chatBuf = nil
								if banned, err := s.db.NoteSpamHit(s.userID, s.remoteIP(), msg, spamHitsToBan); err == nil && banned {
									log.Printf("blocked IP %s after spam from %q", s.remoteIP(), name)
								}
								s.write(render.LobbyChatUpdate(s.lob.GetChat(), nil, true, s.mode == render.ModeASCII))
								break
							}
							if spammy || duplicate {
								chatBuf = nil
								s.write(render.LobbyChatUpdate(s.lob.GetChat(), nil, true, s.mode == render.ModeASCII))
								break
							}
						chatTimes[chatIdx] = now
						chatIdx = (chatIdx + 1) % len(chatTimes)
						lastMsg = msg
						s.lob.SendChat(name, msg)
						s.db.LogChat(s.userID, msg) //nolint:errcheck
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
					fullRefresh()
				case b == 't' || b == 'T':
					chatMode = true
					chatBuf = nil
					s.write(render.LobbyInputUpdate(nil, true, s.mode == render.ModeASCII))
				case b == 'c' || b == 'C':
					roomName := s.promptRoomName(inputCh)
					if roomName == "" {
						lr.Reset()
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
				case b == 0x01: // up arrow
					if len(rooms) > 0 {
						if selectedRoom <= 0 {
							selectedRoom = 0
						} else {
							selectedRoom--
						}
						s.write(lr.SelectionUpdate(rooms, selectedRoom))
					}
				case b == 0x02: // down arrow
					if len(rooms) > 0 {
						if selectedRoom < len(rooms)-1 {
							selectedRoom++
						} else {
							selectedRoom = len(rooms) - 1
						}
						s.write(lr.SelectionUpdate(rooms, selectedRoom))
					}
				case b == '\r' || b == '\n':
					if selectedRoom < 0 || selectedRoom >= len(rooms) {
						continue
					}
					id := rooms[selectedRoom].ID
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
		s.db.RecordGame(s.userID)                                    //nolint:errcheck
		s.db.RecordWallDestroyed(s.userID, g.BlocksDestroyed[slot]) //nolint:errcheck
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
				s.db.RecordWin(s.userID) //nolint:errcheck
			} else {
				s.db.RecordGame(s.userID) //nolint:errcheck
			}
			s.db.RecordWallDestroyed(s.userID, g.BlocksDestroyed[slot]) //nolint:errcheck
			s.db.RecordKill(s.userID, g.KillsBy[slot])                  //nolint:errcheck
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
			var data []byte
			for {
				c, err := s.rd.ReadByte()
				if err != nil {
					return 0, err
				}
				if c == iacByte {
					s.rd.ReadByte() // SE
					break
				}
				data = append(data, c)
			}
			s.handleSubneg(data)
			continue
		}
		if cmd == will || cmd == wont || cmd == do || cmd == dont {
			s.rd.ReadByte() //nolint:errcheck
		}
	}
}
