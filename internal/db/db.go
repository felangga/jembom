package db

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"

	"github.com/felangga/jembom/internal/render"
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
	q.SetMaxOpenConns(1) // SQLite supports one writer at a time
	if err := migrate(q); err != nil {
		q.Close()
		return nil, err
	}
	return &DB{q: q}, nil
}

func migrate(q *sql.DB) error {
	// --- users table ---
	// Check if table exists at all first.
	var usersExists int
	q.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&usersExists) //nolint:errcheck

	if usersExists == 0 {
		// Fresh DB — create directly.
		if _, err := q.Exec(`CREATE TABLE users (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			pin_hash        TEXT NOT NULL,
			wins            INTEGER DEFAULT 0,
			games           INTEGER DEFAULT 0,
			kills           INTEGER DEFAULT 0,
			walls_destroyed INTEGER DEFAULT 0
		)`); err != nil {
			return err
		}
	} else {
		// Existing DB: check for old schema (no id column).
		var hasUserID int
		q.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='id'`).Scan(&hasUserID) //nolint:errcheck
		if hasUserID == 0 {
			tx, err := q.Begin()
			if err != nil {
				return err
			}
			stmts := []string{
				`CREATE TABLE users_new (
					id              INTEGER PRIMARY KEY AUTOINCREMENT,
					name            TEXT NOT NULL UNIQUE,
					pin_hash        TEXT NOT NULL,
					wins            INTEGER DEFAULT 0,
					games           INTEGER DEFAULT 0,
					kills           INTEGER DEFAULT 0,
					walls_destroyed INTEGER DEFAULT 0
				)`,
				`INSERT OR IGNORE INTO users_new (name, pin_hash, wins, games, kills, walls_destroyed)
					SELECT name, pin_hash, wins,
					       COALESCE(games, 0),
					       COALESCE(kills, 0),
					       COALESCE(walls_destroyed, 0)
					FROM users`,
				`DROP TABLE users`,
				`ALTER TABLE users_new RENAME TO users`,
			}
			for _, s := range stmts {
				if _, err := tx.Exec(s); err != nil {
					tx.Rollback() //nolint:errcheck
					return err
				}
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}

	// --- log tables ---
	// Drop and recreate if they use old name-based schema.
	var hasAuthUserID int
	q.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('auth_log') WHERE name='user_id'`).Scan(&hasAuthUserID) //nolint:errcheck
	if hasAuthUserID == 0 {
		for _, t := range []string{"auth_log", "chat_log", "session_log"} {
			q.Exec(`DROP TABLE IF EXISTS ` + t) //nolint:errcheck
		}
	}

	creates := []string{
		`CREATE TABLE IF NOT EXISTS auth_log (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			ts      TEXT    NOT NULL,
			user_id INTEGER REFERENCES users(id),
			ip      TEXT    NOT NULL,
			event   TEXT    NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_log (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			ts      TEXT    NOT NULL,
			user_id INTEGER NOT NULL REFERENCES users(id),
			message TEXT    NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_log (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			connect_ts    TEXT    NOT NULL,
			disconnect_ts TEXT    NOT NULL,
			ip            TEXT    NOT NULL,
			user_id       INTEGER REFERENCES users(id),
			mode          TEXT,
			term_type     TEXT,
			term_w        INTEGER DEFAULT 0,
			term_h        INTEGER DEFAULT 0,
			session_s     INTEGER NOT NULL
		)`,
	}
	for _, s := range creates {
		if _, err := q.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// UserExists returns (userID, true, nil) when found, (0, false, nil) when not.
func (d *DB) UserExists(name string) (int64, bool, error) {
	var id int64
	err := d.q.QueryRow(`SELECT id FROM users WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// Register creates a new user and returns the assigned user_id.
func (d *DB) Register(name, pin string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	res, err := d.q.Exec(`INSERT INTO users (name, pin_hash) VALUES (?, ?)`, name, string(hash))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Verify checks the PIN and returns the user_id on success.
func (d *DB) Verify(userID int64, pin string) error {
	var hash string
	err := d.q.QueryRow(`SELECT pin_hash FROM users WHERE id = ?`, userID).Scan(&hash)
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

func (d *DB) RecordWin(userID int64) error {
	_, err := d.q.Exec(`UPDATE users SET wins = wins + 1, games = games + 1 WHERE id = ?`, userID)
	return err
}

func (d *DB) RecordGame(userID int64) error {
	_, err := d.q.Exec(`UPDATE users SET games = games + 1 WHERE id = ?`, userID)
	return err
}

func (d *DB) RecordKill(userID int64) error {
	_, err := d.q.Exec(`UPDATE users SET kills = kills + 1 WHERE id = ?`, userID)
	return err
}

func (d *DB) RecordWallDestroyed(userID int64, count int) error {
	if count <= 0 {
		return nil
	}
	_, err := d.q.Exec(`UPDATE users SET walls_destroyed = walls_destroyed + ? WHERE id = ?`, count, userID)
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

// LogAuth records an auth event. userID 0 means unknown (stored as NULL).
func (d *DB) LogAuth(userID int64, ip, event string) error {
	_, err := d.q.Exec(
		`INSERT INTO auth_log (ts, user_id, ip, event) VALUES (?, NULLIF(?, 0), ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), userID, ip, event)
	return err
}

func (d *DB) RecentChat(n int) ([]render.ChatMessage, error) {
	rows, err := d.q.Query(
		`SELECT u.name, c.message FROM chat_log c
		 JOIN users u ON u.id = c.user_id
		 ORDER BY c.id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []render.ChatMessage
	for rows.Next() {
		var m render.ChatMessage
		if err := rows.Scan(&m.Name, &m.Text); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse: DB returned newest-first, lobby wants oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (d *DB) LogChat(userID int64, message string) error {
	_, err := d.q.Exec(
		`INSERT INTO chat_log (ts, user_id, message) VALUES (?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), userID, message)
	return err
}

// LogSession records a completed session. userID 0 means not authenticated (stored as NULL).
func (d *DB) LogSession(connectTS, disconnectTS, ip string, userID int64, mode, termType string, termW, termH, sessionS int) error {
	_, err := d.q.Exec(
		`INSERT INTO session_log (connect_ts, disconnect_ts, ip, user_id, mode, term_type, term_w, term_h, session_s)
		 VALUES (?, ?, ?, NULLIF(?, 0), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`,
		connectTS, disconnectTS, ip, userID, mode, termType, termW, termH, sessionS)
	return err
}

func (d *DB) Close() error { return d.q.Close() }
