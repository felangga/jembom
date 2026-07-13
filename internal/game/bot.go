package game

import "math/rand"

const botCooldown = 2 // ticks between moves (200ms)

type Bot struct {
	PlayerID int
	cooldown int
}

func NewBot(playerID int) *Bot {
	return &Bot{PlayerID: playerID, cooldown: rand.Intn(botCooldown)}
}

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

	// 1. Flee if in blast zone.
	if danger[p.Y][p.X] {
		b.flee(g, p, danger)
		return
	}

	// 2. If in a bombing position, bomb and flee.
	//    If in position but can't safely escape, fall through to find a better position.
	if inBombingPos(g, p, p.X, p.Y) {
		if p.BombCount < p.BombMax && b.canEscapeAfterBomb(g, p, danger) {
			g.placeBomb(p)
			b.flee(g, p, buildDangerMap(g))
			return
		}
	}

	// 3. BFS toward the nearest cell that gives a bombing position.
	if b.approachBombingPos(g, p, danger) {
		return
	}

	// 4. Clear a block in the direction of the nearest enemy.
	if b.clearBlockTowardEnemy(g, p, danger) {
		return
	}

	// 5. Wander to a safe cell.
	b.wander(g, p, danger)
}

// inBombingPos returns true if placing a bomb at (x,y) would hit any live enemy
// in line-of-sight within p.BombPower range.
func inBombingPos(g *Game, p *Player, x, y int) bool {
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	for _, d := range dirs {
		for i := 1; i <= p.BombPower; i++ {
			tx, ty := x+d[0]*i, y+d[1]*i
			if tx < 0 || ty < 0 || tx >= MapWidth || ty >= MapHeight {
				break
			}
			if g.Map.Cells[ty][tx] == CellWall || g.Map.Cells[ty][tx] == CellBlock {
				break
			}
			for _, other := range g.Players {
				if other.Alive && other.ID != p.ID && other.X == tx && other.Y == ty {
					return true
				}
			}
		}
	}
	return false
}

// approachBombingPos BFS-searches for the nearest walkable cell from which a
// bomb would hit an enemy, then takes the first step toward it.
func (b *Bot) approachBombingPos(g *Game, p *Player, danger [MapHeight][MapWidth]bool) bool {
	type state struct{ x, y, fx, fy int }
	var visited [MapHeight][MapWidth]bool
	visited[p.Y][p.X] = true

	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })

	var queue []state
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMoveSafe(nx, ny) && !visited[ny][nx] && !danger[ny][nx] {
			visited[ny][nx] = true
			queue = append(queue, state{nx, ny, nx, ny})
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if inBombingPos(g, p, cur.x, cur.y) {
			p.X, p.Y = cur.fx, cur.fy
			return true
		}
		for _, d := range dirs {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight || visited[ny][nx] {
				continue
			}
			if g.canMoveSafe(nx, ny) && !danger[ny][nx] {
				visited[ny][nx] = true
				queue = append(queue, state{nx, ny, cur.fx, cur.fy})
			}
		}
	}
	return false
}

// clearBlockTowardEnemy bombs an adjacent block that is in the direction of the
// nearest enemy. Falls back to any adjacent block if none are toward the enemy.
func (b *Bot) clearBlockTowardEnemy(g *Game, p *Player, danger [MapHeight][MapWidth]bool) bool {
	if p.BombCount >= p.BombMax {
		return false
	}
	target := b.nearestEnemy(g, p)
	if target == nil {
		return false
	}

	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	// Try block in the direction of the enemy first.
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight {
			continue
		}
		if g.Map.Cells[ny][nx] != CellBlock {
			continue
		}
		// Is this block in the general direction of the target?
		dx, dy := target.X-p.X, target.Y-p.Y
		if (d[0] != 0 && isToward(d[0], dx)) || (d[1] != 0 && isToward(d[1], dy)) {
			if b.canEscapeAfterBomb(g, p, danger) {
				g.placeBomb(p)
				b.flee(g, p, buildDangerMap(g))
				return true
			}
		}
	}

	// Fallback: any adjacent block.
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight {
			continue
		}
		if g.Map.Cells[ny][nx] == CellBlock && b.canEscapeAfterBomb(g, p, danger) {
			g.placeBomb(p)
			b.flee(g, p, buildDangerMap(g))
			return true
		}
	}
	return false
}

// isToward returns true if step is in the same direction as delta.
func isToward(step, delta int) bool {
	return (step > 0 && delta > 0) || (step < 0 && delta < 0)
}

// buildDangerMap marks every cell covered by active explosions or predicted bomb blasts.
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

// flee moves one step toward the nearest safe cell.
// First tries a safe adjacent cell directly; only if all adjacent cells are
// dangerous does it BFS through danger to find the closest exit.
func (b *Bot) flee(g *Game, p *Player, danger [MapHeight][MapWidth]bool) {
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })

	// Fast path: safe adjacent cell exists — move there immediately.
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMoveSafe(nx, ny) && !danger[ny][nx] {
			p.X, p.Y = nx, ny
			return
		}
	}

	// Slow path: BFS for nearest safe cell, never through explosion cells.
	type state struct{ x, y, fx, fy int }
	var visited [MapHeight][MapWidth]bool
	visited[p.Y][p.X] = true
	var queue []state
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMoveSafe(nx, ny) && !visited[ny][nx] {
			visited[ny][nx] = true
			queue = append(queue, state{nx, ny, nx, ny})
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if !danger[cur.y][cur.x] {
			p.X, p.Y = cur.fx, cur.fy
			return
		}
		for _, d := range dirs {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx < 0 || ny < 0 || nx >= MapWidth || ny >= MapHeight || visited[ny][nx] {
				continue
			}
			if g.canMoveSafe(nx, ny) {
				visited[ny][nx] = true
				queue = append(queue, state{nx, ny, cur.fx, cur.fy})
			}
		}
	}
	// Truly cornered — stay put.
}

// canEscapeAfterBomb simulates placing a bomb at p's position and checks that
// a safe cell is BFS-reachable within BombTimer/botCooldown steps.
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
				if g.canMoveSafe(nx, ny) {
					visited[ny][nx] = true
					next = append(next, pt{nx, ny})
				}
			}
		}
		queue = next
	}
	return false
}

func (b *Bot) nearestEnemy(g *Game, p *Player) *Player {
	var nearest *Player
	best := 1 << 30
	for _, other := range g.Players {
		if !other.Alive || other.ID == p.ID {
			continue
		}
		dx, dy := other.X-p.X, other.Y-p.Y
		if d := dx*dx + dy*dy; d < best {
			best = d
			nearest = other
		}
	}
	return nearest
}

func (b *Bot) wander(g *Game, p *Player, danger [MapHeight][MapWidth]bool) {
	// Prefer moving toward nearest enemy.
	if target := b.nearestEnemy(g, p); target != nil {
		dx, dy := target.X-p.X, target.Y-p.Y
		var ordered [][2]int
		if dx*dx >= dy*dy {
			if dx > 0 {
				ordered = append(ordered, [2]int{1, 0})
			} else if dx < 0 {
				ordered = append(ordered, [2]int{-1, 0})
			}
			if dy > 0 {
				ordered = append(ordered, [2]int{0, 1})
			} else if dy < 0 {
				ordered = append(ordered, [2]int{0, -1})
			}
		} else {
			if dy > 0 {
				ordered = append(ordered, [2]int{0, 1})
			} else if dy < 0 {
				ordered = append(ordered, [2]int{0, -1})
			}
			if dx > 0 {
				ordered = append(ordered, [2]int{1, 0})
			} else if dx < 0 {
				ordered = append(ordered, [2]int{-1, 0})
			}
		}
		for _, d := range ordered {
			nx, ny := p.X+d[0], p.Y+d[1]
			if g.canMoveSafe(nx, ny) && !danger[ny][nx] {
				p.X, p.Y = nx, ny
				return
			}
		}
	}

	// Fallback: random safe direction.
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		nx, ny := p.X+d[0], p.Y+d[1]
		if g.canMoveSafe(nx, ny) && !danger[ny][nx] {
			p.X, p.Y = nx, ny
			return
		}
	}
}
