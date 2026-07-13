package game

var SpawnPoints = [4][2]int{
	{1, 1},
	{MapWidth - 2, 1},
	{1, MapHeight - 2},
	{MapWidth - 2, MapHeight - 2},
}

var PlayerColors = [4]string{
	"\033[94m", // bright blue
	"\033[91m", // bright red
	"\033[92m", // bright green
	"\033[95m", // bright magenta
}

type Player struct {
	ID        int
	Name      string
	X, Y      int
	Alive     bool
	BombMax   int
	BombCount int
	BombPower int
	Color     string
	IsBot     bool
}

func NewPlayer(id int, name string) *Player {
	return &Player{
		ID:        id,
		Name:      name,
		X:         SpawnPoints[id][0],
		Y:         SpawnPoints[id][1],
		Alive:     true,
		BombMax:   3,
		BombCount: 0,
		BombPower: 2,
		Color:     PlayerColors[id],
	}
}
