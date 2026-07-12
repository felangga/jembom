package render

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/felangga/bbman/internal/game"
)

// Classic BBS screen: 80×25.
const (
	screenW = 80
	screenH = 25
)

// Viewport: inner cells rendered per frame (excluding border chars).
// Map border visual width  = 2(margin) + 1(│) + viewW×2 + 1(│) = viewW×2+4 = 78 ✓
// Layout rows:
//
//	1        title
//	2        ─── separator
//	3        ┌── map top border
//	4…16     map inner rows  (viewH = 13)
//	17       └── map bot border
//	18       blank
//	19       ─── separator
//	20…23    player list  (2 per row × 2 rows)
//	24       controls hint
//	25       cursor park
const (
	viewW      = 37 // inner viewport width in cells
	viewH      = 13 // inner viewport height in cells
	mapRow     = 3  // 1-based screen row of map top border
	playerRow  = 20 // 1-based screen row where player list starts
	controlRow = 24
)

// ANSI helpers.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	gray   = "\033[90m"
	white  = "\033[37m"
	yellow = "\033[93m"
	cyan   = "\033[96m"
	red    = "\033[91m"
	green  = "\033[92m"
)

const resizeTerm = "\033[8;25;80t"

func at(row, col int) string { return fmt.Sprintf("\033[%d;%dH", row, col) }
func cls() string            { return "\033[2J\033[H\033[?25l" + resizeTerm }

// GameFrame renders the full 80×25 game screen for playerID.
// The viewport is centered on that player.
func GameFrame(g *game.Game, playerID int) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())

	// ── Row 1: title + player position hint ────────────────────────────────
	var px, py int
	if playerID >= 0 && playerID < len(g.Players) && g.Players[playerID].Alive {
		px, py = g.Players[playerID].X, g.Players[playerID].Y
	}
	vx, vy := viewport(px, py)
	title := fmt.Sprintf("[ BBMan ]  pos:%d,%d  map:%dx%d  view:%d,%d",
		px, py, game.MapWidth, game.MapHeight, vx, vy)
	buf.WriteString(at(1, 1) + bold+cyan + padCenter(title, screenW) + reset)

	// ── Row 2: separator ───────────────────────────────────────────────────
	buf.WriteString(at(2, 1) + gray + strings.Repeat("─", screenW) + reset)

	// ── Row 3: map top border ──────────────────────────────────────────────
	scrollUp := vy > 0
	scrollDn := vy+viewH < game.MapHeight
	buf.WriteString(at(mapRow, 1))
	buf.WriteString(white + bold + "  ┌")
	buf.WriteString(mapHBorder(scrollUp))
	buf.WriteString("┐" + reset)

	// ── Rows 4…16: map viewport ────────────────────────────────────────────
	grid := buildGrid(g)
	for ry := 0; ry < viewH; ry++ {
		mapY := vy + ry
		buf.WriteString(at(mapRow+1+ry, 1))
		scrollLeft := vx > 0
		scrollRight := vx+viewW < game.MapWidth
		leftGutter := "  │"
		rightGutter := "│"
		if scrollLeft {
			leftGutter = white + bold + "  ◄" + reset
		} else {
			leftGutter = white + bold + "  │" + reset
		}
		if scrollRight {
			rightGutter = white + bold + "►" + reset
		} else {
			rightGutter = white + bold + "│" + reset
		}
		buf.WriteString(leftGutter)
		for rx := 0; rx < viewW; rx++ {
			mapX := vx + rx
			c := grid[mapY][mapX]
			if c.color != "" {
				buf.WriteString(bold + c.color)
			}
			buf.WriteString(c.ch)
			if c.color != "" {
				buf.WriteString(reset)
			}
		}
		buf.WriteString(rightGutter)
	}

	// ── Row 17: map bottom border ──────────────────────────────────────────
	buf.WriteString(at(mapRow+1+viewH, 1))
	buf.WriteString(white + bold + "  └")
	buf.WriteString(mapHBorder(scrollDn))
	buf.WriteString("┘" + reset)

	// ── Row 18: blank / Row 19: separator ─────────────────────────────────
	buf.WriteString(at(19, 1) + gray + strings.Repeat("─", screenW) + reset)

	// ── Rows 20-23: player list (2 per row) ───────────────────────────────
	for i, p := range g.Players {
		row := playerRow + i/2
		col := 1 + (i%2)*40
		buf.WriteString(at(row, col))
		buf.WriteString(renderPlayerLine(p, playerID))
	}

	// ── Row 24: controls ───────────────────────────────────────────────────
	buf.WriteString(at(controlRow, 1))
	buf.WriteString(gray + "  WASD/arrows:move  SPACE/B:bomb  Q:quit" + reset)

	// ── Row 25: cursor park ────────────────────────────────────────────────
	buf.WriteString(at(screenH, 1))
	return buf.Bytes()
}

// viewport returns the top-left map coordinate for the viewport centered on (px,py).
func viewport(px, py int) (vx, vy int) {
	vx = px - viewW/2
	vy = py - viewH/2
	if vx < 0 {
		vx = 0
	}
	if vy < 0 {
		vy = 0
	}
	if vx > game.MapWidth-viewW {
		vx = game.MapWidth - viewW
	}
	if vy > game.MapHeight-viewH {
		vy = game.MapHeight - viewH
	}
	return
}

// mapHBorder returns a horizontal border string with a scroll arrow if needed.
func mapHBorder(hasScroll bool) string {
	inner := strings.Repeat("──", viewW)
	if hasScroll {
		mid := len(inner) / 2
		return inner[:mid-1] + "▲▼" + inner[mid+1:]
	}
	return inner
}

type displayCell struct {
	ch    string // always exactly 2 visual chars
	color string
}

func buildGrid(g *game.Game) [][]displayCell {
	grid := make([][]displayCell, game.MapHeight)
	for y := range grid {
		grid[y] = make([]displayCell, game.MapWidth)
		for x := range grid[y] {
			switch g.Map.Cells[y][x] {
			case game.CellWall:
				grid[y][x] = displayCell{wallChars(x, y, g), white}
			case game.CellBlock:
				grid[y][x] = displayCell{"▒▒", yellow}
			default:
				grid[y][x] = displayCell{"  ", ""}
			}
		}
	}
	for _, e := range g.Explosions {
		grid[e.Y][e.X] = displayCell{"░░", "\033[93m"}
	}
	for _, b := range g.Bombs {
		grid[b.Y][b.X] = displayCell{"()", red}
	}
	for _, p := range g.Players {
		if p.Alive {
			grid[p.Y][p.X] = displayCell{fmt.Sprintf("@%d", p.ID+1), p.Color}
		}
	}
	return grid
}

// wallChars picks a box-drawing character pair for a wall cell based on its
// four cardinal neighbors, producing visually connected wall lines.
func wallChars(mapX, mapY int, g *game.Game) string {
	isWall := func(x, y int) bool {
		if x < 0 || y < 0 || x >= game.MapWidth || y >= game.MapHeight {
			return true // treat out-of-bounds as wall
		}
		return g.Map.Cells[y][x] == game.CellWall
	}

	u := isWall(mapX, mapY-1)
	d := isWall(mapX, mapY+1)
	l := isWall(mapX-1, mapY)
	r := isWall(mapX+1, mapY)

	// Build a 4-bit index: bit0=up, bit1=down, bit2=left, bit3=right.
	idx := 0
	if u {
		idx |= 1
	}
	if d {
		idx |= 2
	}
	if l {
		idx |= 4
	}
	if r {
		idx |= 8
	}

	// Junction character for each neighbor combination.
	junctions := [16]string{
		"■", // 0000  isolated
		"║", // 0001  up
		"║", // 0010  down
		"║", // 0011  up+down
		"═", // 0100  left
		"╝", // 0101  up+left
		"╗", // 0110  down+left
		"╣", // 0111  up+down+left
		"═", // 1000  right
		"╚", // 1001  up+right
		"╔", // 1010  down+right
		"╠", // 1011  up+down+right
		"═", // 1100  left+right
		"╩", // 1101  up+left+right
		"╦", // 1110  down+left+right
		"╬", // 1111  all four
	}

	// Second char: horizontal extension when right neighbor is also a wall.
	ext := " "
	if r {
		ext = "═"
	}
	return junctions[idx] + ext
}

func renderPlayerLine(p *game.Player, myID int) string {
	color := p.Color
	status := green + "ALIVE" + reset
	if !p.Alive {
		color = gray
		status = red + "DEAD " + reset
	}
	tag := ""
	if p.ID == myID {
		tag = yellow + " ◄YOU" + reset
	} else if p.IsBot {
		tag = gray + " [CPU]" + reset
	}
	bombs := gray + strings.Repeat("o", p.BombMax) + reset
	return fmt.Sprintf("%s%s[%d] %-12s%s %s  %s%s",
		bold, color, p.ID+1, p.Name, reset, status, bombs, tag)
}

// Welcome renders the 80×25 welcome/name-entry screen.
func Welcome() []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW)

	art := []string{
		`██████╗ ██████╗ ███╗   ███╗ █████╗ ███╗   ██╗`,
		`██╔══██╗██╔══██╗████╗ ████║██╔══██╗████╗  ██║`,
		`██████╦╝██████╔╝██╔████╔██║███████║██╔██╗ ██║`,
		`██╔══██╗██╔══██╗██║╚██╔╝██║██╔══██║██║╚██╗██║`,
		`██████╔╝██████╔╝██║ ╚═╝ ██║██║  ██║██║ ╚████║`,
		`╚═════╝ ╚═════╝ ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═══╝`,
	}
	startRow := 5
	for i, line := range art {
		buf.WriteString(at(startRow+i, 2) + bold+cyan + padCenter(line, screenW-2) + reset)
	}
	buf.WriteString(at(startRow+len(art)+2, 2) + gray + padCenter("Retro BBS Bomberman  ::  multiplayer", screenW-2) + reset)
	buf.WriteString(at(startRow+len(art)+5, 2) + bold + padCenter("Enter your name:", screenW-2) + reset)
	inputCol := (screenW-14)/2 + 1
	buf.WriteString(at(startRow+len(art)+6, inputCol) + "> ")
	buf.WriteString("\033[?25h")
	return buf.Bytes()
}

// Lobby renders the 80×25 waiting lobby screen.
func Lobby(playerName string, waiting []string) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW)
	buf.WriteString(at(2, 2) + bold+cyan + padCenter("[ BBMan Lobby ]", screenW-2) + reset)
	buf.WriteString(at(4, 4) + "Welcome, " + bold+yellow + playerName + reset + "!")
	buf.WriteString(at(6, 4) + gray + "Waiting for players..." + reset)
	buf.WriteString(at(8, 4) + bold + "Players in lobby:" + reset)
	for i, name := range waiting {
		color := game.PlayerColors[i%4]
		buf.WriteString(at(9+i, 6) + bold+color + fmt.Sprintf("[%d]", i+1) + reset + " " + name)
	}
	buf.WriteString(at(18, 4) + gray + "Game starts automatically when 2–4 players are ready." + reset)
	buf.WriteString(at(screenH, 1))
	return buf.Bytes()
}

// GameOver renders the 80×25 game-over screen.
func GameOver(winnerName string, isWinner bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW)
	if isWinner {
		buf.WriteString(at(10, 2) + bold+yellow + padCenter("** YOU WIN! **", screenW-2) + reset)
	} else {
		buf.WriteString(at(10, 2) + bold+red + padCenter("GAME OVER", screenW-2) + reset)
		if winnerName != "" {
			buf.WriteString(at(12, 2) + bold+white + padCenter("Winner: "+winnerName, screenW-2) + reset)
		} else {
			buf.WriteString(at(12, 2) + bold+yellow + padCenter("DRAW!", screenW-2) + reset)
		}
	}
	buf.WriteString(at(16, 2) + gray + padCenter("Press any key to play again...", screenW-2) + reset)
	buf.WriteString(at(screenH, 1))
	return buf.Bytes()
}

func drawBox(buf *bytes.Buffer, row, col, h, w int) {
	buf.WriteString(white + bold)
	buf.WriteString(at(row, col) + "┌" + strings.Repeat("─", w-2) + "┐")
	for r := row + 1; r < row+h-1; r++ {
		buf.WriteString(at(r, col) + "│" + at(r, col+w-1) + "│")
	}
	buf.WriteString(at(row+h-1, col) + "└" + strings.Repeat("─", w-2) + "┘")
	buf.WriteString(reset)
}

func padCenter(s string, w int) string {
	pad := (w - len(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}
