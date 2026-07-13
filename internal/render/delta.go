package render

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/felangga/jembom/internal/game"
)

// DeltaRenderer tracks per-player frame state for incremental screen updates.
// Instead of redrawing the full 80×25 screen every tick, it emits only the
// cells that changed since the previous frame — critical for slow terminals.
type DeltaRenderer struct {
	mu               sync.Mutex
	prevGrid         [][]displayCell
	prevVX           int
	prevVY           int
	prevPX           int
	prevPY           int
	prevPlayers      []playerSnap
	prevExplosions   map[[2]int]bool
	ascii            bool
	first            bool
}

type playerSnap struct {
	name    string
	alive   bool
	bombMax int
	isBot   bool
	color   string
}

func NewDeltaRenderer(ascii bool) *DeltaRenderer {
	return &DeltaRenderer{ascii: ascii, first: true}
}

// Frame writes a minimal update to w for the given player.
// Falls back to a full redraw when the viewport shifts or on first call.
func (dr *DeltaRenderer) Frame(g *game.Game, playerID int, w io.Writer) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	var px, py int
	if playerID >= 0 && playerID < len(g.Players) && g.Players[playerID].Alive {
		px, py = g.Players[playerID].X, g.Players[playerID].Y
	}
	vx, vy := viewport(px, py)

	if dr.first || vx != dr.prevVX || vy != dr.prevVY {
		w.Write(GameFrame(g, playerID, dr.ascii)) //nolint:errcheck
		dr.prevGrid = buildGrid(g, dr.ascii)
		dr.prevVX, dr.prevVY = vx, vy
		dr.prevPX, dr.prevPY = px, py
		dr.prevPlayers = snapPlayers(g)
		dr.prevExplosions = snapshotExplosions(g)
		dr.first = false
		return
	}

	newGrid := buildGrid(g, dr.ascii)
	var buf bytes.Buffer
	buf.WriteString("\033[?25l") // hide cursor before any cell updates

	// Map cells — only emit changed cells within current viewport.
	for ry := 0; ry < viewH; ry++ {
		mapY := vy + ry
		for rx := 0; rx < viewW; rx++ {
			mapX := vx + rx
			nc := newGrid[mapY][mapX]
			oc := dr.prevGrid[mapY][mapX]
			if nc.ch == oc.ch && nc.color == oc.color {
				continue
			}
			// Col 4 is where map cells start (2 spaces + 1 border char).
			buf.WriteString(at(mapRow+1+ry, 4+rx*2) + reset)
			if nc.color != "" {
				buf.WriteString(bold + nc.color + nc.ch + reset)
			} else {
				buf.WriteString(nc.ch)
			}
		}
	}
	dr.prevGrid = newGrid

	// Header line: update when position changes.
	if px != dr.prevPX || py != dr.prevPY {
		title := fmt.Sprintf("[ Jembom ]  pos: %d, %d", px, py)
		buf.WriteString(centerAt(1, title) + bold + cyan + title + reset)
		dr.prevPX, dr.prevPY = px, py
	}

	// Player info rows (rows 20-21): redraw half-row when state changes.
	newSnap := snapPlayers(g)
	for i := range newSnap {
		if i >= len(g.Players) {
			break
		}
		old := playerSnap{}
		if i < len(dr.prevPlayers) {
			old = dr.prevPlayers[i]
		}
		if newSnap[i] != old {
			row := playerRow + i/2
			col := 1 + (i%2)*40
			buf.WriteString(at(row, col) + strings.Repeat(" ", 39))
			buf.WriteString(at(row, col))
			buf.WriteString(renderPlayerLine(g.Players[i], playerID, dr.ascii))
		}
	}
	dr.prevPlayers = newSnap

	// Beep (BEL) when new explosion cells appear.
	newExp := snapshotExplosions(g)
	for pos := range newExp {
		if !dr.prevExplosions[pos] {
			buf.WriteByte(0x07) // BEL — one beep per explosion event
			break
		}
	}
	dr.prevExplosions = newExp

	buf.WriteString("\033[?25l")
	if buf.Len() > 0 {
		w.Write(buf.Bytes()) //nolint:errcheck
	}
}

func snapshotExplosions(g *game.Game) map[[2]int]bool {
	m := make(map[[2]int]bool, len(g.Explosions))
	for _, e := range g.Explosions {
		m[[2]int{e.X, e.Y}] = true
	}
	return m
}

func snapPlayers(g *game.Game) []playerSnap {
	snaps := make([]playerSnap, len(g.Players))
	for i, p := range g.Players {
		snaps[i] = playerSnap{
			name:    p.Name,
			alive:   p.Alive,
			bombMax: p.BombMax,
			isBot:   p.IsBot,
			color:   p.Color,
		}
	}
	return snaps
}
