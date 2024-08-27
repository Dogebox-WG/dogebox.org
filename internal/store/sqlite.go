package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"code.dogecoin.org/dkm/internal"
	"github.com/mattn/go-sqlite3"
)

const SQL_SCHEMA string = `
CREATE TABLE IF NOT EXISTS config (
	id INTEGER PRIMARY KEY,
	s1 BLOB NOT NULL,
	s2 BLOB NOT NULL,
	enc BLOB NOT NULL
);
`

type SQLiteStore struct {
	db *sql.DB
}

type SQLiteStoreCtx struct {
	_db *sql.DB
	ctx context.Context
}

func New(filename string) (internal.Store, error) {
	backend := "sqlite3"
	db, err := sql.Open(backend, filename)
	store := &SQLiteStore{db: db}
	if err != nil {
		return store, dbErr(err, "opening database")
	}
	setup_sql := SQL_SCHEMA
	if backend == "sqlite3" {
		// limit concurrent access until we figure out a way to start transactions
		// with the BEGIN CONCURRENT statement in Go.
		db.SetMaxOpenConns(1)
	}
	// init tables / indexes
	_, err = db.Exec(setup_sql)
	if err != nil {
		return store, dbErr(err, "creating database schema")
	}
	return store, err
}

func (s *SQLiteStore) Close() {
	s.db.Close()
}

func (s *SQLiteStore) WithCtx(ctx context.Context) internal.StoreCtx {
	return &SQLiteStoreCtx{
		_db: s.db,
		ctx: ctx,
	}
}

func IsConflict(err error) bool {
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			return true
		}
	}
	return false
}

func (s SQLiteStoreCtx) doTxn(name string, work func(tx *sql.Tx) error) error {
	db := s._db
	limit := 120
	for {
		tx, err := db.Begin()
		if err != nil {
			if IsConflict(err) {
				s.Sleep(250 * time.Millisecond)
				limit--
				if limit != 0 {
					continue
				}
			}
			return fmt.Errorf("[Store] cannot begin transaction: %w", err)
		}
		defer tx.Rollback()
		err = work(tx)
		if err != nil {
			if IsConflict(err) {
				s.Sleep(250 * time.Millisecond)
				limit--
				if limit != 0 {
					continue
				}
			}
			return err
		}
		err = tx.Commit()
		if err != nil {
			if IsConflict(err) {
				s.Sleep(250 * time.Millisecond)
				limit--
				if limit != 0 {
					continue
				}
			}
			return fmt.Errorf("[Store] cannot commit %v: %w", name, err)
		}
		return nil
	}
}

func (s SQLiteStoreCtx) Sleep(dur time.Duration) {
	select {
	case <-s.ctx.Done():
	case <-time.After(dur):
	}
}

func dbErr(err error, where string) error {
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrConstraint {
			// MUST detect 'AlreadyExists' to fulfil the API contract!
			// Constraint violation, e.g. a duplicate key.
			return internal.WrapErr(internal.AlreadyExists, "SQLiteStore: already-exists", err)
		}
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			// SQLite has a single-writer policy, even in WAL (write-ahead) mode.
			// SQLite will return BUSY if the database is locked by another connection.
			// We treat this as a transient database conflict, and the caller should retry.
			return internal.WrapErr(internal.DBConflict, "SQLiteStore: db-conflict", err)
		}
	}
	return internal.WrapErr(internal.DBProblem, fmt.Sprintf("SQLiteStore: db-problem: %s", where), err)
}

// STORE INTERFACE

func (s SQLiteStoreCtx) SetMaster(id int, s1 []byte, s2 []byte, enc []byte, allowReplace bool) error {
	return s.doTxn("SetMaster", func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO config (id,s1,s2,enc) VALUES (?,?,?,?)", id, s1, s2, enc)
		if err != nil {
			if errors.Is(err, sqlite3.ErrConstraint) && allowReplace {
				// already exists and allowed to replace.
				_, err = tx.Exec("UPDATE config SET s1=?,s2=?,enc=? WHERE id=?", s1, s2, enc, id)
				if err != nil {
					return dbErr(err, "SetMaster")
				}
				return nil
			}
			return dbErr(err, "SetMaster") // AlreadyExists or error
		}
		return nil
	})
}

func (s SQLiteStoreCtx) GetMaster(id int) (s1 []byte, s2 []byte, enc []byte, err error) {
	err = s.doTxn("GetMaster", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT s1,s2,enc FROM config LIMIT 1")
		return row.Scan(&s1, &s2, &enc)
	})
	return
}
