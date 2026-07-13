package game

import "math/rand"

type CellType uint8

const (
	CellEmpty CellType = iota
	CellWall            // indestructible
	CellBlock           // destructible
)

const (
	MapWidth  = 37
	MapHeight = 15
)

type Map struct {
	Cells [MapHeight][MapWidth]CellType
}

func NewMap() *Map {
	m := &Map{}
	m.generate()
	return m
}

func (m *Map) generate() {
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			if x == 0 || y == 0 || x == MapWidth-1 || y == MapHeight-1 {
				m.Cells[y][x] = CellWall
			} else if x%2 == 0 && y%2 == 0 {
				m.Cells[y][x] = CellWall
			} else if isSpawnArea(x, y) {
				m.Cells[y][x] = CellEmpty
			} else if rand.Intn(10) < 7 {
				m.Cells[y][x] = CellBlock
			}
		}
	}
}

func isSpawnArea(x, y int) bool {
	safe := [][2]int{
		{1, 1}, {2, 1}, {1, 2},
		{MapWidth - 2, 1}, {MapWidth - 3, 1}, {MapWidth - 2, 2},
		{1, MapHeight - 2}, {2, MapHeight - 2}, {1, MapHeight - 3},
		{MapWidth - 2, MapHeight - 2}, {MapWidth - 3, MapHeight - 2}, {MapWidth - 2, MapHeight - 3},
	}
	for _, c := range safe {
		if c[0] == x && c[1] == y {
			return true
		}
	}
	return false
}

// SpawnPoint returns the corner spawn position for the given player slot (0-3).
func SpawnPoint(playerID int) (int, int) {
	switch playerID {
	case 0:
		return 1, 1
	case 1:
		return MapWidth - 2, 1
	case 2:
		return 1, MapHeight - 2
	default:
		return MapWidth - 2, MapHeight - 2
	}
}

func (m *Map) IsWalkable(x, y int) bool {
	if x < 0 || y < 0 || x >= MapWidth || y >= MapHeight {
		return false
	}
	return m.Cells[y][x] == CellEmpty
}
