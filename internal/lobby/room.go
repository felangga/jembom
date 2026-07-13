package lobby

import (
	"fmt"
	"io"
	"sync"

	"github.com/felangga/bbman/internal/game"
	"github.com/felangga/bbman/internal/render"
)

type Room struct {
	id   int
	name string

	mu       sync.Mutex
	finished bool
	botSlots []int
	waiters  []*Waiter
	game     *game.Game
	players  []*game.Player

	renderersMu sync.RWMutex
	renderers   [maxPlayers]*render.DeltaRenderer

	onFinish func(id int)
}

func newRoom(id int, name string, onFinish func(int)) *Room {
	return &Room{id: id, name: name, onFinish: onFinish}
}

func (r *Room) info() render.RoomInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return render.RoomInfo{
		ID:         r.id,
		Name:       r.name,
		HumanCount: len(r.waiters),
	}
}

// Start creates the game (filling remaining slots with bots), signals creator's
// ReadyCh, then blocks until game over.
func (r *Room) Start(creator *Waiter) {
	players := make([]*game.Player, maxPlayers)
	var bots []*game.Bot
	var botSlots []int

	players[0] = game.NewPlayer(0, creator.Name)
	for i := 1; i < maxPlayers; i++ {
		p := game.NewPlayer(i, fmt.Sprintf("CPU%d", i+1))
		p.IsBot = true
		players[i] = p
		bot := game.NewBot(i)
		bots = append(bots, bot)
		botSlots = append(botSlots, i)
	}

	r.renderersMu.Lock()
	r.renderers[0] = render.NewDeltaRenderer(creator.ASCII)
	r.renderersMu.Unlock()

	var g *game.Game
	renderFn := func(gg *game.Game) {
		gg.EachWriter(func(playerID int, w io.Writer) {
			r.renderersMu.RLock()
			dr := r.renderers[playerID]
			r.renderersMu.RUnlock()
			if dr != nil {
				dr.Frame(gg, playerID, w)
			} else {
				w.Write(render.GameFrame(gg, playerID, gg.ASCIIMode(playerID))) //nolint:errcheck
			}
		})
	}

	g = game.New(players, renderFn)
	for _, b := range bots {
		g.AddBot(b)
	}
	g.SetWriter(0, creator.Writer)
	g.SetASCIIMode(0, creator.ASCII)

	r.mu.Lock()
	r.game = g
	r.players = players
	r.waiters = []*Waiter{creator}
	r.botSlots = botSlots
	creator.Slot = 0
	creator.Game = g
	creator.Room = r
	r.mu.Unlock()

	close(creator.ReadyCh)

	g.Start() // ── blocking game loop ──────────────────────────────────────

	r.mu.Lock()
	allWaiters := r.waiters
	r.waiters = nil
	r.finished = true
	winner := g.Winner
	r.mu.Unlock()

	winnerName := ""
	if winner >= 0 && winner < len(players) {
		winnerName = players[winner].Name
	}
	for _, w := range allWaiters {
		w.Writer.Write(render.GameOver(winnerName, winner == w.Slot, w.ASCII)) //nolint:errcheck
		close(w.DoneCh)
	}

	r.onFinish(r.id)
}

// LeaveEarly removes a waiter from the room so they don't receive GameOver,
// and unblocks JoinRunning if it is waiting on w.DoneCh.
func (r *Room) LeaveEarly(w *Waiter) {
	r.mu.Lock()
	out := r.waiters[:0]
	for _, v := range r.waiters {
		if v != w {
			out = append(out, v)
		}
	}
	r.waiters = out
	r.mu.Unlock()
	close(w.DoneCh) // unblocks JoinRunning's <-w.DoneCh if applicable
}

// JoinRunning replaces one bot slot with the incoming human player.
// Prefers dead bot slots so the new player is revived. Returns false if no
// bot slots remain or the room is finished.
func (r *Room) JoinRunning(w *Waiter) bool {
	r.mu.Lock()

	if r.finished || len(r.botSlots) == 0 {
		r.mu.Unlock()
		return false
	}

	// Prefer a dead bot slot so the player gets revived at spawn.
	g := r.game
	slot := -1
	chosenIdx := 0
	for i, s := range r.botSlots {
		if !g.IsAlive(s) {
			slot = s
			chosenIdx = i
			break
		}
	}
	if slot == -1 {
		slot = r.botSlots[0]
		chosenIdx = 0
	}
	r.botSlots = append(r.botSlots[:chosenIdx], r.botSlots[chosenIdx+1:]...)
	r.waiters = append(r.waiters, w)
	w.Slot = slot
	w.Game = g
	w.Room = r
	r.mu.Unlock()

	r.renderersMu.Lock()
	r.renderers[slot] = render.NewDeltaRenderer(w.ASCII)
	r.renderersMu.Unlock()

	g.TakeOverBot(slot, w.Name, w.Writer)
	g.SetASCIIMode(slot, w.ASCII)
	close(w.ReadyCh)
	<-w.DoneCh
	return true
}
