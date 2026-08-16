package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Todo is the shape served at /api/todos.
type Todo struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	Position  int64  `json:"position"`
	CreatedAt string `json:"createdAt"`
}

type todoStore struct {
	db *sql.DB
}

func newTodoStore(path string) (*todoStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc.org/sqlite: keep writes serialized
	const schema = `
CREATE TABLE IF NOT EXISTS todos (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	text TEXT NOT NULL,
	done INTEGER NOT NULL DEFAULT 0,
	position INTEGER NOT NULL,
	created_at TEXT NOT NULL
);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &todoStore{db: db}, nil
}

func (s *todoStore) list() ([]Todo, error) {
	rows, err := s.db.Query(`SELECT id, text, done, position, created_at FROM todos ORDER BY position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Todo{}
	for rows.Next() {
		var t Todo
		var done int
		if err := rows.Scan(&t.ID, &t.Text, &done, &t.Position, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Done = done != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *todoStore) create(text string) (Todo, error) {
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM todos`).Scan(&maxPos); err != nil {
		return Todo{}, err
	}
	pos := maxPos.Int64 + 1
	createdAt := time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(`INSERT INTO todos (text, done, position, created_at) VALUES (?, 0, ?, ?)`,
		text, pos, createdAt)
	if err != nil {
		return Todo{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Todo{}, err
	}
	return Todo{ID: id, Text: text, Done: false, Position: pos, CreatedAt: createdAt}, nil
}

func (s *todoStore) setDone(id int64, done bool) error {
	res, err := s.db.Exec(`UPDATE todos SET done = ? WHERE id = ?`, boolToInt(done), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("todo %d not found", id)
	}
	return nil
}

func (s *todoStore) delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM todos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("todo %d not found", id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
