package pgindex

import "database/sql"

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
