package db

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"

	"github.com/felangga/bbman/internal/render"
)

var ErrWrongPIN = errors.New("wrong pin")
var ErrNotFound = errors.New("user not found")

type DB struct {
	q *sql.DB
}

func Open(path string) (*DB, error) {
	q, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = q.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			name            TEXT PRIMARY KEY,
			pin_hash        TEXT NOT NULL,
			wins            INTEGER DEFAULT 0,
			games           INTEGER DEFAULT 0,
			kills           INTEGER DEFAULT 0,
			walls_destroyed INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS auth_log (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			ts        TEXT NOT NULL,
			name      TEXT NOT NULL,
			ip        TEXT NOT NULL,
			event     TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS chat_log (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			ts        TEXT NOT NULL,
			name      TEXT NOT NULL,
			message   TEXT NOT NULL
		)
	`)
	if err == nil {
		// Add column to existing DBs — harmless if already present.
		q.Exec(`ALTER TABLE users ADD COLUMN walls_destroyed INTEGER DEFAULT 0`) //nolint:errcheck
	}
	if err != nil {
		return nil, err
	}
	return &DB{q: q}, nil
}

func (d *DB) UserExists(name string) (bool, error) {
	var n int
	err := d.q.QueryRow(`SELECT COUNT(*) FROM users WHERE name = ?`, name).Scan(&n)
	return n > 0, err
}

func (d *DB) Register(name, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = d.q.Exec(`INSERT INTO users (name, pin_hash) VALUES (?, ?)`, name, string(hash))
	return err
}

func (d *DB) Verify(name, pin string) error {
	var hash string
	err := d.q.QueryRow(`SELECT pin_hash FROM users WHERE name = ?`, name).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)); err != nil {
		return ErrWrongPIN
	}
	return nil
}

func (d *DB) RecordWin(name string) error {
	_, err := d.q.Exec(`UPDATE users SET wins = wins + 1, games = games + 1 WHERE name = ?`, name)
	return err
}

func (d *DB) RecordGame(name string) error {
	_, err := d.q.Exec(`UPDATE users SET games = games + 1 WHERE name = ?`, name)
	return err
}

func (d *DB) RecordKill(name string) error {
	_, err := d.q.Exec(`UPDATE users SET kills = kills + 1 WHERE name = ?`, name)
	return err
}

func (d *DB) RecordWallDestroyed(name string, count int) error {
	if count <= 0 {
		return nil
	}
	_, err := d.q.Exec(
		`UPDATE users SET walls_destroyed = walls_destroyed + ? WHERE name = ?`, count, name)
	return err
}

func (d *DB) TopPlayers(n int) ([]render.LeaderEntry, error) {
	rows, err := d.q.Query(
		`SELECT name, wins, games, walls_destroyed FROM users ORDER BY wins DESC, games ASC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []render.LeaderEntry
	rank := 1
	for rows.Next() {
		var e render.LeaderEntry
		e.Rank = rank
		rank++
		if err := rows.Scan(&e.Name, &e.Wins, &e.Games, &e.WallsDestroyed); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) LogAuth(name, ip, event string) error {
	_, err := d.q.Exec(
		`INSERT INTO auth_log (ts, name, ip, event) VALUES (?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), name, ip, event)
	return err
}

func (d *DB) LogChat(name, message string) error {
	_, err := d.q.Exec(
		`INSERT INTO chat_log (ts, name, message) VALUES (?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), name, message)
	return err
}

func (d *DB) Close() error { return d.q.Close() }
