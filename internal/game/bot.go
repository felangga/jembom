package game

import "math/rand"

// botCooldown = ticks between bot moves. At 100ms/tick → 500ms per move.
const botCooldown = 5

// Bot is an AI-controlled player.
type Bot struct {
	PlayerID int
	cooldown int
}

func NewBot(playerID int) *Bot {
	return &Bot{
		PlayerID: playerID,
		cooldown: rand.Intn(botCooldown), // stagger initial moves
	}
}

// Tick runs one AI decision step. Called with game lock held.
func (b *Bot) Tick(g *Game) {
	p := g.Players[b.PlayerID]
	if !p.Alive {
		return
	}

	b.cooldown--
	if b.cooldown > 0 {
		return
	}
	b.cooldown = botCooldown

	danger := buildDangerMap(g)

	// Priority 1: flee current cell if it's in a blast zone.
	if danger[p.Y][p.X] {
		b.flee(g, p, danger)
		return
	}

	// Priority 2: place bomb when adjacent to target and escape exists.
	if b.adjacentToTarget(g, p) && b.canEscapeAfterBomb(g, p, danger) {
		g.placeBomb(p)
		return
	}

	// Priority 3: BFS toward nearest player.
	if !b.moveToward(g, p, danger) {
		b.wander(g, p, danger)
	}
}

// buildDangerMap marks every cell covered by active explosions or any bomb's
// predicted blast zone.
func buildDangerMap(g *Game) [MapHeight][MapWidth]bool {
	var dm [MapHeight][MapWidth]bool

	for _, e := range g.Explosions {
		dm[e.Y][e.X] = true
	}

	dirs := [][2]int{{0, 0}, {0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	for _, bomb := range g.Bombs {
		for _, d := range dirs {
			for i := 0; i <= bomb.Power; i++ {
				ex, ey := bomb.X+d[0]*i, bomb.Y+d[1]*i
				if ex < 0 || ey < 0 || ex >= MapWidth || ey >= MapHeight {
					break
				}
				cell := g.Map.Cells[ey][ex]
				if cell == CellWall {
					break
				}
				dm[ey][ex] = true
				if cell == CellBlock {
					break
				}
			}
		}
	}
	return dm
}

// flee moves the bot to any safe adjacent cell. Falls back to any walkable
// cell if everything is in danger.
func (b *Bot) flee(g *Game, p *Player, danger [MapHeight][MapWidth]bool) {
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })

	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMove(nx, ny) && !danger[ny][nx] {
			p.X, p.Y = nx, ny
			return
		}
	}
	// Cornered — move anywhere walkable.
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMove(nx, ny) {
			p.X, p.Y = nx, ny
			return
		}
	}
}

// adjacentToTarget returns true when the bot can usefully place a bomb:
// there is a destructible block or enemy player in one of the four cardinal cells.
func (b *Bot) adjacentToTarget(g *Game, p *Player) bool {
	if p.BombCount >= p.BombMax {
		return false
	}
	for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		nx, ny := p.X+d[0], p.Y+d[1]
		if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight {
			continue
		}
		if g.Map.Cells[ny][nx] == CellBlock {
			return true
		}
		for _, other := range g.Players {
			if other.Alive && other.ID != p.ID && other.X == nx && other.Y == ny {
				return true
			}
		}
	}
	return false
}

// canEscapeAfterBomb simulates placing a bomb at the bot's position and checks
// that a safe cell is reachable within BombTimer/botCooldown steps.
func (b *Bot) canEscapeAfterBomb(g *Game, p *Player, existingDanger [MapHeight][MapWidth]bool) bool {
	sim := existingDanger
	dirs := [][2]int{{0, 0}, {0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	for _, d := range dirs {
		for i := 0; i <= p.BombPower; i++ {
			ex, ey := p.X+d[0]*i, p.Y+d[1]*i
			if ex < 0 || ey < 0 || ex >= MapWidth || ey >= MapHeight {
				break
			}
			cell := g.Map.Cells[ey][ex]
			if cell == CellWall {
				break
			}
			sim[ey][ex] = true
			if cell == CellBlock {
				break
			}
		}
	}

	type pt struct{ x, y int }
	var visited [MapHeight][MapWidth]bool
	visited[p.Y][p.X] = true
	queue := []pt{{p.X, p.Y}}
	maxSteps := BombTimer/botCooldown + 1

	for step := 0; step <= maxSteps && len(queue) > 0; step++ {
		var next []pt
		for _, cur := range queue {
			if !sim[cur.y][cur.x] {
				return true
			}
			for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
				nx, ny := cur.x+d[0], cur.y+d[1]
				if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight || visited[ny][nx] {
					continue
				}
				if g.Map.IsWalkable(nx, ny) {
					visited[ny][nx] = true
					next = append(next, pt{nx, ny})
				}
			}
		}
		queue = next
	}
	return false
}

// moveToward does BFS toward the nearest alive player, avoiding danger cells.
// Returns true if a move was made.
func (b *Bot) moveToward(g *Game, p *Player, danger [MapHeight][MapWidth]bool) bool {
	target := b.nearestPlayer(g, p)
	if target == nil {
		return false
	}

	type state struct {
		x, y         int
		firstX, firstY int
	}

	var visited [MapHeight][MapWidth]bool
	visited[p.Y][p.X] = true

	var queue []state
	for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMove(nx, ny) && !visited[ny][nx] && !danger[ny][nx] {
			visited[ny][nx] = true
			queue = append(queue, state{nx, ny, nx, ny})
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.x == target.X && cur.y == target.Y {
			p.X, p.Y = cur.firstX, cur.firstY
			return true
		}

		for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight || visited[ny][nx] {
				continue
			}
			if !g.canMove(nx, ny) || danger[ny][nx] {
				continue
			}
			visited[ny][nx] = true
			queue = append(queue, state{nx, ny, cur.firstX, cur.firstY})
		}
	}
	return false
}

func (b *Bot) nearestPlayer(g *Game, p *Player) *Player {
	var nearest *Player
	bestDist := 1 << 30
	for _, other := range g.Players {
		if !other.Alive || other.ID == p.ID {
			continue
		}
		dx, dy := other.X-p.X, other.Y-p.Y
		if d := dx*dx + dy*dy; d < bestDist {
			bestDist = d
			nearest = other
		}
	}
	return nearest
}

func (b *Bot) wander(g *Game, p *Player, danger [MapHeight][MapWidth]bool) {
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMove(nx, ny) && !danger[ny][nx] {
			p.X, p.Y = nx, ny
			return
		}
	}
}
