package game

const (
	BombTimer         = 30 // ticks (~3 seconds at 100ms/tick)
	ExplosionDuration = 5  // ticks
)

type Bomb struct {
	X, Y    int
	Owner   int
	Timer   int
	Power   int
}

type Explosion struct {
	X, Y  int
	Timer int
}

func NewBomb(x, y, owner, power int) *Bomb {
	return &Bomb{
		X:     x,
		Y:     y,
		Owner: owner,
		Timer: BombTimer,
		Power: power,
	}
}
