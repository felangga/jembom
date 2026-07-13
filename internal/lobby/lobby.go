package lobby

import (
	"io"
	"sync"

	"github.com/felangga/jembom/internal/game"
	"github.com/felangga/jembom/internal/render"
)

const maxPlayers = 4

// Waiter is created by the session and passed to Lobby.CreateRoom / JoinRoom.
type Waiter struct {
	Name    string
	Writer  io.Writer
	ReadyCh chan struct{} // closed by lobby when Game/Slot are ready
	DoneCh  chan struct{} // closed by lobby when the game ends
	Game    *game.Game
	Slot    int
	ASCII   bool
	Room    *Room // set by CreateRoom/JoinRoom; used for LeaveEarly
}

const maxChatHistory = 20

// LobbyHandle is returned by RegisterViewer and used to receive lobby updates.
type LobbyHandle struct {
	Name     string
	UpdateCh chan struct{} // non-blocking signal: new chat arrived
}

type Lobby struct {
	mu      sync.Mutex
	rooms   []*Room
	nextID  int
	chat    []render.ChatMessage
	viewers []*LobbyHandle
}

func New() *Lobby { return &Lobby{} }

func (l *Lobby) SeedChat(msgs []render.ChatMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chat = append(msgs, l.chat...)
	if len(l.chat) > maxChatHistory {
		l.chat = l.chat[len(l.chat)-maxChatHistory:]
	}
}

func (l *Lobby) RegisterViewer(name string) *LobbyHandle {
	h := &LobbyHandle{Name: name, UpdateCh: make(chan struct{}, 1)}
	l.mu.Lock()
	l.viewers = append(l.viewers, h)
	l.mu.Unlock()
	return h
}

func (l *Lobby) UnregisterViewer(h *LobbyHandle) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.viewers[:0]
	for _, v := range l.viewers {
		if v != h {
			out = append(out, v)
		}
	}
	l.viewers = out
}

func (l *Lobby) OnlineCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.viewers)
	for _, r := range l.rooms {
		n += len(r.waiters)
	}
	return n
}

func (l *Lobby) GetChat() []render.ChatMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]render.ChatMessage, len(l.chat))
	copy(out, l.chat)
	return out
}

func (l *Lobby) SendChat(name, text string) {
	l.mu.Lock()
	l.chat = append(l.chat, render.ChatMessage{Name: name, Text: text})
	if len(l.chat) > maxChatHistory {
		l.chat = l.chat[len(l.chat)-maxChatHistory:]
	}
	viewers := make([]*LobbyHandle, len(l.viewers))
	copy(viewers, l.viewers)
	l.mu.Unlock()

	for _, v := range viewers {
		select {
		case v.UpdateCh <- struct{}{}:
		default:
		}
	}
}

// GetRooms returns a snapshot of current room info.
func (l *Lobby) GetRooms() []render.RoomInfo {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]render.RoomInfo, len(l.rooms))
	for i, r := range l.rooms {
		out[i] = r.info()
	}
	return out
}

// CreateRoom starts a new room with w as the first (and only human) player.
// Blocks until the game ends. Returns false only if the lobby is at capacity.
func (l *Lobby) CreateRoom(roomName string, w *Waiter) {
	l.mu.Lock()
	id := l.nextID
	l.nextID++
	r := newRoom(id, roomName, l.removeRoom)
	l.rooms = append(l.rooms, r)
	l.mu.Unlock()

	r.Start(w) // blocks until game over
}

// JoinRoom attempts to join an existing room by ID.
// Returns false if the room does not exist or is full/finished.
func (l *Lobby) JoinRoom(roomID int, w *Waiter) bool {
	l.mu.Lock()
	var target *Room
	for _, r := range l.rooms {
		if r.id == roomID {
			target = r
			break
		}
	}
	l.mu.Unlock()

	if target == nil {
		return false
	}
	return target.JoinRunning(w)
}

func (l *Lobby) removeRoom(id int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	filtered := l.rooms[:0]
	for _, r := range l.rooms {
		if r.id != id {
			filtered = append(filtered, r)
		}
	}
	l.rooms = filtered
}
