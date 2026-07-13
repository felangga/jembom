package game

import (
	"io"
	"sync"
	"time"
)

type State int

const (
	StatePlaying State = iota
	StateOver
)

type Input struct {
	PlayerID int
	Key      byte
}

type Game struct {
	mu         sync.Mutex
	Map        *Map
	Players    []*Player
	Bombs      []*Bomb
	Explosions []*Explosion
	State      State
	Tick       int
	Winner          int // -1 = draw
	BlocksDestroyed []int

	inputCh  chan Input
	doneCh   chan struct{}
	RenderFn func(*Game)
	bots     []*Bot

	// playerWriters holds one io.Writer per player slot (nil = bot/empty).
	// Protected by writersMu, which is always acquired OUTSIDE g.mu to
	// avoid inversion with the game-loop lock.
	writersMu     sync.RWMutex
	playerWriters []io.Writer
	asciiMode     []bool
}

func New(players []*Player, renderFn func(*Game)) *Game {
	return &Game{
		Map:             NewMap(),
		Players:         players,
		State:           StatePlaying,
		Winner:          -1,
		inputCh:         make(chan Input, 64),
		doneCh:          make(chan struct{}),
		RenderFn:        renderFn,
		playerWriters:   make([]io.Writer, len(players)),
		BlocksDestroyed: make([]int, len(players)),
		asciiMode:       make([]bool, len(players)),
	}
}

func (g *Game) AddBot(b *Bot) {
	g.bots = append(g.bots, b)
}

// SetWriter registers (or replaces) the io.Writer for playerID.
func (g *Game) SetWriter(playerID int, w io.Writer) {
	g.writersMu.Lock()
	if playerID >= 0 && playerID < len(g.playerWriters) {
		g.playerWriters[playerID] = w
	}
	g.writersMu.Unlock()
}

// SetASCIIMode sets the ASCII rendering flag for playerID.
func (g *Game) SetASCIIMode(playerID int, ascii bool) {
	g.writersMu.Lock()
	if playerID >= 0 && playerID < len(g.asciiMode) {
		g.asciiMode[playerID] = ascii
	}
	g.writersMu.Unlock()
}

// ASCIIMode returns the ASCII rendering flag for playerID.
func (g *Game) ASCIIMode(playerID int) bool {
	g.writersMu.RLock()
	defer g.writersMu.RUnlock()
	if playerID >= 0 && playerID < len(g.asciiMode) {
		return g.asciiMode[playerID]
	}
	return false
}

// EachWriter calls fn for every registered human player writer (snapshot copy).
func (g *Game) EachWriter(fn func(playerID int, w io.Writer)) {
	g.writersMu.RLock()
	snap := make([]io.Writer, len(g.playerWriters))
	copy(snap, g.playerWriters)
	g.writersMu.RUnlock()

	for i, w := range snap {
		if w != nil {
			fn(i, w)
		}
	}
}

// TakeOverBot atomically removes the bot at playerID and registers the human
// writer. Safe to call concurrently with the game loop.
func (g *Game) TakeOverBot(playerID int, name string, w io.Writer) {
	g.mu.Lock()
	filtered := g.bots[:0]
	for _, b := range g.bots {
		if b.PlayerID != playerID {
			filtered = append(filtered, b)
		}
	}
	g.bots = filtered
	p := g.Players[playerID]
	p.Name = name
	p.IsBot = false
	if !p.Alive {
		p.Alive = true
		p.X, p.Y = SpawnPoint(playerID)
	}
	g.mu.Unlock()

	// Set writer outside g.mu to avoid lock-order inversion with renderFn.
	g.SetWriter(playerID, w)
}

func (g *Game) Start() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			g.mu.Lock()
			g.drainInputs()
			g.update()
			over := g.State == StateOver
			g.mu.Unlock()

			// Render outside the game-state lock so that TakeOverBot
			// (which also needs g.mu) never blocks for a full frame.
			g.RenderFn(g)

			if over {
				return
			}
		case <-g.doneCh:
			return
		}
	}
}

func (g *Game) IsAlive(playerID int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if playerID < 0 || playerID >= len(g.Players) {
		return false
	}
	return g.Players[playerID].Alive
}

func (g *Game) Stop() {
	select {
	case <-g.doneCh:
	default:
		close(g.doneCh)
	}
}

func (g *Game) SendInput(playerID int, key byte) {
	select {
	case g.inputCh <- Input{PlayerID: playerID, Key: key}:
	default:
	}
}

func (g *Game) drainInputs() {
	for {
		select {
		case in := <-g.inputCh:
			g.handleInput(in)
		default:
			return
		}
	}
}

func (g *Game) handleInput(in Input) {
	if in.PlayerID < 0 || in.PlayerID >= len(g.Players) {
		return
	}
	p := g.Players[in.PlayerID]
	if !p.Alive {
		return
	}
	nx, ny := p.X, p.Y
	switch in.Key {
	case 'w', 'W', keyUp:
		ny--
	case 's', 'S', keyDown:
		ny++
	case 'a', 'A', keyLeft:
		nx--
	case 'd', 'D', keyRight:
		nx++
	case ' ', 'b', 'B':
		g.placeBomb(p)
		return
	case 'q', 'Q':
		p.Alive = false
		return
	}
	if g.canMove(nx, ny) {
		p.X, p.Y = nx, ny
	}
}

// Synthetic single-byte keycodes for arrow keys (translated by session layer).
const (
	keyUp    = 0x01
	keyDown  = 0x02
	keyLeft  = 0x03
	keyRight = 0x04
)

func (g *Game) canMove(x, y int) bool {
	if !g.Map.IsWalkable(x, y) {
		return false
	}
	for _, b := range g.Bombs {
		if b.X == x && b.Y == y {
			return false
		}
	}
	return true
}

func (g *Game) canMoveSafe(x, y int) bool {
	if !g.canMove(x, y) {
		return false
	}
	for _, e := range g.Explosions {
		if e.X == x && e.Y == y {
			return false
		}
	}
	return true
}

func (g *Game) placeBomb(p *Player) {
	if p.BombCount >= p.BombMax {
		return
	}
	for _, b := range g.Bombs {
		if b.X == p.X && b.Y == p.Y {
			return
		}
	}
	p.BombCount++
	g.Bombs = append(g.Bombs, NewBomb(p.X, p.Y, p.ID, p.BombPower))
}

func (g *Game) update() {
	g.Tick++

	for _, bot := range g.bots {
		bot.Tick(g)
	}

	remaining := g.Bombs[:0]
	for _, b := range g.Bombs {
		b.Timer--
		if b.Timer <= 0 {
			g.explodeBomb(b)
			if b.Owner >= 0 && b.Owner < len(g.Players) {
				g.Players[b.Owner].BombCount--
			}
		} else {
			remaining = append(remaining, b)
		}
	}
	g.Bombs = remaining

	remExp := g.Explosions[:0]
	for _, e := range g.Explosions {
		e.Timer--
		if e.Timer > 0 {
			remExp = append(remExp, e)
		}
	}
	g.Explosions = remExp

	for _, p := range g.Players {
		if !p.Alive {
			continue
		}
		for _, e := range g.Explosions {
			if e.X == p.X && e.Y == p.Y {
				p.Alive = false
				break
			}
		}
	}

	alive, lastAlive := 0, -1
	aliveHumans := 0
	for _, p := range g.Players {
		if p.Alive {
			alive++
			lastAlive = p.ID
			if !p.IsBot {
				aliveHumans++
			}
		}
	}
	if alive <= 1 {
		g.Winner = lastAlive
		g.State = StateOver
	} else if aliveHumans == 0 {
		g.Winner = -1 // all remaining players are bots — end as draw
		g.State = StateOver
	}
}

func (g *Game) explodeBomb(b *Bomb) {
	dirs := [][2]int{{0, 0}, {0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	for _, d := range dirs {
		if d[0] == 0 && d[1] == 0 {
			g.addExplosion(b.X, b.Y)
			continue
		}
		for i := 1; i <= b.Power; i++ {
			ex, ey := b.X+d[0]*i, b.Y+d[1]*i
			if ex < 0 || ey < 0 || ex >= MapWidth || ey >= MapHeight {
				break
			}
			cell := g.Map.Cells[ey][ex]
			if cell == CellWall {
				break
			}
			g.addExplosion(ex, ey)
			if cell == CellBlock {
				g.Map.Cells[ey][ex] = CellEmpty
				if b.Owner >= 0 && b.Owner < len(g.BlocksDestroyed) {
					g.BlocksDestroyed[b.Owner]++
				}
				break
			}
		}
	}
	for _, other := range g.Bombs {
		if other == b {
			continue
		}
		for _, e := range g.Explosions {
			if e.X == other.X && e.Y == other.Y {
				other.Timer = 0
			}
		}
	}
}

func (g *Game) addExplosion(x, y int) {
	g.Explosions = append(g.Explosions, &Explosion{X: x, Y: y, Timer: ExplosionDuration})
}
