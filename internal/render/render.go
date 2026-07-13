package render

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/felangga/jembom/internal/game"
)

const (
	screenW = 80
	screenH = 25
)

const (
	viewW      = 37
	viewH      = 15
	mapRow     = 3
	playerRow  = 21
	controlRow = 25
)

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

// sel picks ASCII or Unicode string based on mode.
func sel(ascii bool, a, u string) string {
	if ascii {
		return a
	}
	return u
}

// GameFrame renders the full 80×25 game screen for playerID.
func GameFrame(g *game.Game, playerID int, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())

	var px, py int
	if playerID >= 0 && playerID < len(g.Players) && g.Players[playerID].Alive {
		px, py = g.Players[playerID].X, g.Players[playerID].Y
	}
	vx, vy := viewport(px, py)
	const title = "[ Jembom ]"
	buf.WriteString(centerAt(1, title) + bold + cyan + title + reset)
	buf.WriteString(at(2, 1) + gray + strings.Repeat(sel(ascii, "-", "─"), screenW) + reset)

	scrollUp := vy > 0
	scrollDn := vy+viewH < game.MapHeight
	buf.WriteString(at(mapRow, 1))
	buf.WriteString(white + bold + "  " + sel(ascii, "+", "┌"))
	buf.WriteString(mapHBorder(scrollUp, ascii))
	buf.WriteString(sel(ascii, "+", "┐") + reset)

	grid := buildGrid(g, ascii)
	vl := sel(ascii, "|", "│")
	lArr := sel(ascii, "<", "◄")
	rArr := sel(ascii, ">", "►")
	for ry := 0; ry < viewH; ry++ {
		mapY := vy + ry
		buf.WriteString(at(mapRow+1+ry, 1))
		scrollLeft := vx > 0
		scrollRight := vx+viewW < game.MapWidth
		if scrollLeft {
			buf.WriteString(white + bold + "  " + lArr + reset)
		} else {
			buf.WriteString(white + bold + "  " + vl + reset)
		}
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
		if scrollRight {
			buf.WriteString(white + bold + rArr + reset)
		} else {
			buf.WriteString(white + bold + vl + reset)
		}
	}

	buf.WriteString(at(mapRow+1+viewH, 1))
	buf.WriteString(white + bold + "  " + sel(ascii, "+", "└"))
	buf.WriteString(mapHBorder(scrollDn, ascii))
	buf.WriteString(sel(ascii, "+", "┘") + reset)

	buf.WriteString(at(playerRow-1, 1) + gray + strings.Repeat(sel(ascii, "-", "─"), screenW) + reset)

	for i, p := range g.Players {
		row := playerRow + i/2
		col := 1 + (i%2)*40
		buf.WriteString(at(row, col))
		buf.WriteString(renderPlayerLine(p, playerID, ascii))
	}

	buf.WriteString(at(controlRow, 1))
	buf.WriteString(gray + "  WASD/arrows:move  SPACE/B:bomb  Q:quit" + reset)
	buf.WriteString("\033[?25l")
	return buf.Bytes()
}

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

func mapHBorder(hasScroll, ascii bool) string {
	h := sel(ascii, "-", "─")
	inner := strings.Repeat(h+h, viewW)
	if hasScroll {
		mid := len(inner) / 2
		return inner[:mid-1] + sel(ascii, "^v", "▲▼") + inner[mid+1:]
	}
	return inner
}

type displayCell struct {
	ch    string
	color string
}

func buildGrid(g *game.Game, ascii bool) [][]displayCell {
	grid := make([][]displayCell, game.MapHeight)
	for y := range grid {
		grid[y] = make([]displayCell, game.MapWidth)
		for x := range grid[y] {
			switch g.Map.Cells[y][x] {
			case game.CellWall:
				grid[y][x] = displayCell{wallChars(x, y, g, ascii), white}
			case game.CellBlock:
				grid[y][x] = displayCell{sel(ascii, "[]", "▒▒"), yellow}
			default:
				grid[y][x] = displayCell{"  ", ""}
			}
		}
	}
	for _, e := range g.Explosions {
		grid[e.Y][e.X] = displayCell{sel(ascii, "**", "░░"), "\033[93m"}
	}
	for _, b := range g.Bombs {
		secs := (b.Timer + 9) / 10
		grid[b.Y][b.X] = displayCell{fmt.Sprintf("%2d", secs), red}
	}
	for _, p := range g.Players {
		if p.Alive {
			grid[p.Y][p.X] = displayCell{fmt.Sprintf("@%d", p.ID+1), p.Color}
		}
	}
	return grid
}

func wallChars(mapX, mapY int, g *game.Game, ascii bool) string {
	if ascii {
		return "##"
	}
	isWall := func(x, y int) bool {
		if x < 0 || y < 0 || x >= game.MapWidth || y >= game.MapHeight {
			return true
		}
		return g.Map.Cells[y][x] == game.CellWall
	}
	u := isWall(mapX, mapY-1)
	d := isWall(mapX, mapY+1)
	l := isWall(mapX-1, mapY)
	r := isWall(mapX+1, mapY)
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
	junctions := [16]string{
		"■", "║", "║", "║",
		"═", "╝", "╗", "╣",
		"═", "╚", "╔", "╠",
		"═", "╩", "╦", "╬",
	}
	ext := " "
	if r {
		ext = "═"
	}
	return junctions[idx] + ext
}

func renderPlayerLine(p *game.Player, myID int, ascii bool) string {
	color := p.Color
	status := green + "ALIVE" + reset
	if !p.Alive {
		color = gray
		status = red + "DEAD " + reset
	}
	tag := ""
	if p.ID == myID {
		tag = yellow + sel(ascii, " <YOU", " ◄YOU") + reset
	} else if p.IsBot {
		tag = gray + " [CPU]" + reset
	}
	bombs := gray + strings.Repeat("o", p.BombMax) + reset
	return fmt.Sprintf("%s%s[%d] %-12s%s %s  %s%s",
		bold, color, p.ID+1, p.Name, reset, status, bombs, tag)
}

// RoomInfo is a snapshot of a room's public state.
type RoomInfo struct {
	ID         int
	Name       string
	HumanCount int
}

// LeaderEntry is one row in the leaderboard.
type LeaderEntry struct {
	Rank           int
	Name           string
	Score          int
	Wins           int
	Games          int
	Kills          int
	WallsDestroyed int
}

// ChatMessage is one lobby chat line.
type ChatMessage struct {
	Name string
	Text string
}

const (
	lobbyDivCol   = 41
	chatHeaderRow = 18
	chatFirstRow  = 19
	chatLineCount = 5
	lobbyInputRow = 24
)

// PinPrompt renders the 80×25 PIN entry screen.
func PinPrompt(playerName, prompt, errMsg string, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW, ascii)
	const authTitle = "[ Jembom - Auth ]"
	buf.WriteString(centerAt(2, authTitle) + bold + cyan + authTitle + reset)
	welcome := "Welcome, " + playerName
	buf.WriteString(centerAt(5, welcome) + bold + welcome + reset)
	buf.WriteString(centerAt(10, prompt) + bold + prompt + reset)
	inputCol := (screenW-8)/2 + 1
	buf.WriteString(at(11, inputCol) + "PIN: ")
	if errMsg != "" {
		buf.WriteString(centerAt(13, errMsg) + red + errMsg + reset)
	}
	buf.WriteString("\033[?25h")
	buf.WriteString(at(11, inputCol+5))
	return buf.Bytes()
}

// LobbyScreen renders the full 80×25 lobby.
func LobbyScreen(playerName string, rooms []RoomInfo, leaders []LeaderEntry, chat []ChatMessage, inputBuf []byte, chatMode bool, ascii bool, selectedRoom, onlineCount int) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawLobbyBorder(&buf, ascii)

	lobbyTitle := "[ Jembom Lobby ]"
	buf.WriteString(at(1, (screenW-utf8.RuneCountInString(lobbyTitle))/2+1) + bold + cyan + lobbyTitle + reset)
	buf.WriteString(LobbyOnlineLine(onlineCount))
	buf.WriteString(at(3, 2) + bold + "ROOMS" + reset)
	buf.WriteString(at(3, 43) + bold + "LEADERBOARD" + reset)
	buf.WriteString(at(4, 2) + gray + fmt.Sprintf("  %-2s  %-16s  %-5s", "#", "Name", "Player") + reset)
	buf.WriteString(at(4, 43) + gray + fmt.Sprintf("%-3s %-10s %5s %4s %4s %4s", "#", "Name", "Score", "Win", "Kill", "Wall") + reset)
	buf.WriteString(at(5, 2) + gray + strings.Repeat(sel(ascii, "-", "─"), 37) + reset)
	buf.WriteString(at(5, 43) + gray + strings.Repeat(sel(ascii, "-", "─"), 36) + reset)

	for i := 0; i < 10; i++ {
		row := 6 + i
		if i < len(rooms) {
			buf.WriteString(at(row, 2) + RoomRow(rooms[i], i == selectedRoom))
		}
		if i < len(leaders) {
			l := leaders[i]
			buf.WriteString(at(row, 43) + fmt.Sprintf("%2d  %-10s %5d %4d %4d %4d",
				l.Rank, truncate(l.Name, 10), l.Score, l.Wins, l.Kills, l.WallsDestroyed))
		}
	}
	if len(rooms) == 0 {
		buf.WriteString(at(8, 4) + gray + "No rooms yet." + reset)
	}

	buf.Write(lobbyChatSection(chat, inputBuf, chatMode, ascii))
	buf.WriteString(lobbyCursorPark(inputBuf, chatMode))
	return buf.Bytes()
}

// LobbyOnlineLine renders the online count on line 1, right-aligned.
func LobbyOnlineLine(onlineCount int) string {
	label := fmt.Sprintf("%d online", onlineCount)
	col := screenW - utf8.RuneCountInString(label)
	return at(1, col) + bold + green + label + reset
}

// RoomRow renders a single room list entry (38 chars wide).
func RoomRow(r RoomInfo, selected bool) string {
	pColor := green
	if r.HumanCount >= 4 {
		pColor = red
	}
	cursor := "  "
	nameStyle := ""
	if selected {
		cursor = ">>"
		nameStyle = bold + cyan
	}
	return fmt.Sprintf("%s%s%2d  %-16s%s  %s%d/4%s",
		cursor, nameStyle, r.ID, truncate(r.Name, 16), reset, pColor, r.HumanCount, reset)
}

// LobbyChatUpdate redraws rows 18-24.
func LobbyChatUpdate(chat []ChatMessage, inputBuf []byte, chatMode bool, ascii bool) []byte {
	var buf bytes.Buffer
	buf.Write(lobbyChatSection(chat, inputBuf, chatMode, ascii))
	buf.WriteString(lobbyCursorPark(inputBuf, chatMode))
	return buf.Bytes()
}

// LobbyInputUpdate redraws chat header + input line.
func LobbyInputUpdate(inputBuf []byte, chatMode bool, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(at(chatHeaderRow, 2) + strings.Repeat(" ", screenW-2))
	focusArrow := sel(ascii, ">", "►")
	if chatMode {
		buf.WriteString(at(chatHeaderRow, 2) + bold + yellow + focusArrow + " CHAT" + reset +
			gray + "  ESC to exit" + reset)
	} else {
		buf.WriteString(at(chatHeaderRow, 2) + bold + "CHAT" + reset +
			gray + "  T to focus" + reset)
	}
	buf.Write(lobbyInputLine(inputBuf, chatMode, ascii))
	buf.WriteString(lobbyCursorPark(inputBuf, chatMode))
	return buf.Bytes()
}

func lobbyChatSection(chat []ChatMessage, inputBuf []byte, chatMode bool, ascii bool) []byte {
	var buf bytes.Buffer
	focusArrow := sel(ascii, ">", "►")
	buf.WriteString(at(chatHeaderRow, 2) + strings.Repeat(" ", screenW-2))
	if chatMode {
		buf.WriteString(at(chatHeaderRow, 2) + bold + yellow + focusArrow + " CHAT" + reset +
			gray + "  ESC to exit" + reset)
	} else {
		buf.WriteString(at(chatHeaderRow, 2) + bold + "CHAT" + reset +
			gray + "  T to focus" + reset)
	}

	start := 0
	if len(chat) > chatLineCount {
		start = len(chat) - chatLineCount
	}
	msgs := chat[start:]
	for i := 0; i < chatLineCount; i++ {
		row := chatFirstRow + i
		buf.WriteString(at(row, 2) + strings.Repeat(" ", screenW-2))
		if i < len(msgs) {
			m := msgs[i]
			buf.WriteString(at(row, 2) + bold + cyan + truncate(m.Name, 10) + reset + ": " + truncate(m.Text, 62))
		}
	}
	buf.Write(lobbyInputLine(inputBuf, chatMode, ascii))
	return buf.Bytes()
}

func lobbyCursorPark(inputBuf []byte, chatMode bool) string {
	if chatMode {
		return at(lobbyInputRow, 11+len(inputBuf))
	}
	return "\033[?25l"
}

func lobbyInputLine(inputBuf []byte, chatMode bool, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(at(lobbyInputRow, 2) + strings.Repeat(" ", screenW-2))
	if chatMode {
		buf.WriteString(at(lobbyInputRow, 2) + yellow + "[CHAT]" + reset + " > " + string(inputBuf))
	} else {
		arrows := sel(ascii, "^v", "↑↓")
		buf.WriteString(at(lobbyInputRow, 2) + gray + arrows + ":select  Enter:join  C:create  T:chat  Q:quit" + reset)
		if len(inputBuf) > 0 {
			buf.WriteString(at(lobbyInputRow, 60) + bold + "> " + string(inputBuf) + reset)
		}
	}
	buf.WriteString("\033[?25h")
	return buf.Bytes()
}

func drawLobbyBorder(buf *bytes.Buffer, ascii bool) {
	h := sel(ascii, "-", "─")
	v := sel(ascii, "|", "│")
	tl := sel(ascii, "+", "┌")
	tr := sel(ascii, "+", "┐")
	bl := sel(ascii, "+", "└")
	br := sel(ascii, "+", "┘")
	tLeft := sel(ascii, "+", "├")
	tRight := sel(ascii, "+", "┤")
	tTop := sel(ascii, "+", "┬")
	tBot := sel(ascii, "+", "┴")

	buf.WriteString(white + bold)
	buf.WriteString(at(1, 1) + tl + strings.Repeat(h, 78) + tr)
	buf.WriteString(at(2, 1) + tLeft + strings.Repeat(h, 39) + tTop + strings.Repeat(h, 38) + tRight)
	for r := 3; r <= 16; r++ {
		buf.WriteString(at(r, 1) + v + at(r, lobbyDivCol) + v + at(r, screenW) + v)
	}
	buf.WriteString(at(17, 1) + tLeft + strings.Repeat(h, 39) + tBot + strings.Repeat(h, 38) + tRight)
	for r := 18; r <= 24; r++ {
		buf.WriteString(at(r, 1) + v + at(r, screenW) + v)
	}
	buf.WriteString(at(screenH, 1) + bl + strings.Repeat(h, 78) + br)
	buf.WriteString(reset)
}

// RoomNamePrompt renders the room-name input screen.
func RoomNamePrompt(playerName string, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW, ascii)
	const createTitle = "[ Jembom - Create Room ]"
	buf.WriteString(centerAt(2, createTitle) + bold + cyan + createTitle + reset)
	const roomNameLabel = "Room name:"
	buf.WriteString(centerAt(10, roomNameLabel) + bold + roomNameLabel + reset)
	inputCol := (screenW-14)/2 + 1
	buf.WriteString(at(11, inputCol) + "> ")
	buf.WriteString("\033[?25h")
	buf.WriteString(at(11, inputCol+2))
	return buf.Bytes()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "."
}

// Welcome renders the 80×25 welcome/name-entry screen.
func Welcome(ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW, ascii)

	startRow := 5
	if ascii {
		lines := []string{
			"  _ ___ __  __ ___   ___  __  __ ",
			" | | __|  \\/  | _ ) / _ \\|  \\/  |",
			" | |_|_ | |\\/| | _ \\| (_) | |\\/| |",
			" \\__|___|_|  |_|___/  \\___/|_|  |_|",
		}
		for i, line := range lines {
			buf.WriteString(centerAt(startRow+i, line) + bold + cyan + line + reset)
		}
		startRow += len(lines)
	} else {
		art := []string{
			`     ██╗███████╗███╗   ███╗██████╗  ██████╗ ███╗   ███╗`,
			`     ██║██╔════╝████╗ ████║██╔══██╗██╔═══██╗████╗ ████║`,
			`     ██║█████╗  ██╔████╔██║██████╔╝██║   ██║██╔████╔██║`,
			`██   ██║██╔══╝  ██║╚██╔╝██║██╔══██╗██║   ██║██║╚██╔╝██║`,
			`╚█████╔╝███████╗██║ ╚═╝ ██║██████╔╝╚██████╔╝██║ ╚═╝ ██║`,
			` ╚════╝ ╚══════╝╚═╝     ╚═╝╚═════╝  ╚═════╝ ╚═╝     ╚═╝`,
		}
		for i, line := range art {
			buf.WriteString(centerAt(startRow+i, line) + bold + cyan + line + reset)
		}
		startRow += len(art)
	}

	const subtitle = "Retro BBS Bomberman  ::  multiplayer"
	buf.WriteString(centerAt(startRow+2, subtitle) + gray + subtitle + reset)
	const nameLabel = "Enter your name: (4-8 characters)"
	buf.WriteString(centerAt(startRow+5, nameLabel) + bold + nameLabel + reset)
	inputCol := (screenW-14)/2 + 1
	buf.WriteString(at(startRow+6, inputCol) + "> ")
	buf.WriteString("\033[?25h")
	return buf.Bytes()
}

// WelcomeError redraws the error line and reparks the cursor after the typed name.
func WelcomeError(msg string, nameLen int, ascii bool) []byte {
	var buf bytes.Buffer
	startRow := 5
	if ascii {
		startRow += 4
	} else {
		startRow += 6
	}
	errRow := startRow + 8
	buf.WriteString(at(errRow, 2) + strings.Repeat(" ", screenW-2))
	buf.WriteString(centerAt(errRow, msg) + red + msg + reset)
	inputRow := startRow + 6
	inputCol := (screenW-14)/2 + 1 + 2 // after "> "
	buf.WriteString(at(inputRow, inputCol+nameLen))
	return buf.Bytes()
}

// YouDied renders an interim screen shown while the rest of the game plays out.
func YouDied(ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteByte(0x07) // BEL
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW, ascii)
	const diedMsg = "YOU DIED!"
	buf.WriteString(centerAt(10, diedMsg) + bold + red + diedMsg + reset)
	const diedSub = "Press ESC to return to lobby..."
	buf.WriteString(centerAt(13, diedSub) + gray + diedSub + reset)
	buf.WriteString("\033[?25l")
	return buf.Bytes()
}

// GameOver renders the 80×25 game-over screen.
func GameOver(winnerName string, isWinner bool, isDraw bool, ascii bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(cls())
	drawBox(&buf, 1, 1, screenH, screenW, ascii)
	if isWinner {
		buf.WriteString("\x07\x07\x07") // 3 BELs for win
		const winMsg = "** YOU WIN! **"
		buf.WriteString(centerAt(10, winMsg) + bold + yellow + winMsg + reset)
	} else {
		buf.WriteByte(0x07) // 1 BEL for loss
		const lossMsg = "GAME OVER"
		buf.WriteString(centerAt(10, lossMsg) + bold + red + lossMsg + reset)
		if winnerName != "" {
			winner := "Winner: " + winnerName
			buf.WriteString(centerAt(12, winner) + bold + white + winner + reset)
		} else if isDraw {
			const drawMsg = "DRAW!"
			buf.WriteString(centerAt(12, drawMsg) + bold + yellow + drawMsg + reset)
		}
	}
	const escMsg = "Press ESC to continue..."
	buf.WriteString(centerAt(16, escMsg) + gray + escMsg + reset)
	buf.WriteString("\033[?25l")
	return buf.Bytes()
}

func drawBox(buf *bytes.Buffer, row, col, h, w int, ascii bool) {
	tl := sel(ascii, "+", "┌")
	tr := sel(ascii, "+", "┐")
	bl := sel(ascii, "+", "└")
	br := sel(ascii, "+", "┘")
	hl := sel(ascii, "-", "─")
	vl := sel(ascii, "|", "│")
	buf.WriteString(white + bold)
	buf.WriteString(at(row, col) + tl + strings.Repeat(hl, w-2) + tr)
	for r := row + 1; r < row+h-1; r++ {
		buf.WriteString(at(r, col) + vl + at(r, col+w-1) + vl)
	}
	buf.WriteString(at(row+h-1, col) + bl + strings.Repeat(hl, w-2) + br)
	buf.WriteString(reset)
}

func padCenter(s string, w int) string {
	pad := (w - utf8.RuneCountInString(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}

// centerAt returns an ANSI cursor-position sequence that places s centered on
// the 80-column screen, without emitting any leading spaces.
func centerAt(row int, s string) string {
	col := (screenW-utf8.RuneCountInString(s))/2 + 1
	if col < 1 {
		col = 1
	}
	return at(row, col)
}
