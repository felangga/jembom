package lobby

import (
	"fmt"
	"io"
	"sync"

	"github.com/felangga/bbman/internal/game"
	"github.com/felangga/bbman/internal/render"
)

const maxPlayers = 4

// Waiter is created by the session layer and passed to Lobby.Join.
type Waiter struct {
	Name    string
	Writer  io.Writer
	ReadyCh chan struct{} // closed by lobby when game is assigned (Game/Slot are set)
	DoneCh  chan struct{} // closed by lobby when game is over
	Game    *game.Game
	Slot    int
}

type Lobby struct {
	mu      sync.Mutex
	waiting []*Waiter
}

func New() *Lobby { return &Lobby{} }

// Join adds the player to the queue and blocks until their game finishes.
func (l *Lobby) Join(w *Waiter) {
	l.mu.Lock()
	l.waiting = append(l.waiting, w)
	l.broadcastLobby()

	if len(l.waiting) >= 1 {
		group := l.waiting
		l.waiting = nil
		l.mu.Unlock()
		l.runGame(group) // blocks until game over
		return
	}
	l.mu.Unlock()

	// Waiter is not the trigger — block until the game is fully done.
	<-w.DoneCh
}

func (l *Lobby) broadcastLobby() {
	names := make([]string, len(l.waiting))
	for i, w := range l.waiting {
		names[i] = w.Name
	}
	for _, w := range l.waiting {
		w.Writer.Write(render.Lobby(w.Name, names)) //nolint:errcheck
	}
}

func (l *Lobby) runGame(group []*Waiter) {
	players := make([]*game.Player, maxPlayers)

	// Human players.
	for i, w := range group {
		players[i] = game.NewPlayer(i, w.Name)
	}

	// Fill remaining slots with CPU bots.
	var bots []*game.Bot
	for i := len(group); i < maxPlayers; i++ {
		p := game.NewPlayer(i, fmt.Sprintf("CPU%d", i+1))
		p.IsBot = true
		players[i] = p
		bots = append(bots, game.NewBot(i))
	}

	renderFn := func(g *game.Game) {
		for i, w := range group {
			w.Writer.Write(render.GameFrame(g, i)) //nolint:errcheck
		}
	}

	g := game.New(players, renderFn)
	for _, bot := range bots {
		g.AddBot(bot)
	}

	// Signal all waiters that the game is ready.
	for i, w := range group {
		w.Game = g
		w.Slot = i
		close(w.ReadyCh)
	}

	// Run the game loop (blocking).
	g.Start()

	// Render game over screen to human players.
	winnerName := ""
	if g.Winner >= 0 && g.Winner < maxPlayers {
		winnerName = players[g.Winner].Name
	}
	for i, w := range group {
		w.Writer.Write(render.GameOver(winnerName, g.Winner == i)) //nolint:errcheck
	}

	// Unblock any waiting Join calls.
	for _, w := range group {
		close(w.DoneCh)
	}
}
