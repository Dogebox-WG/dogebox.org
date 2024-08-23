package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"code.dogecoin.org/dkm/internal"
	"github.com/mattn/go-sqlite3"
)

const SQL_SCHEMA string = `
CREATE TABLE IF NOT EXISTS config (
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
			return fmt.Errorf("[Store] cannot begin transaction: %v", err)
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
			return fmt.Errorf("[Store] %v: %v", name, err)
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
			return fmt.Errorf("[Store] cannot commit %v: %v", name, err)
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
			return WrapErr(AlreadyExists, "SQLiteStore: already-exists", err)
		}
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			// SQLite has a single-writer policy, even in WAL (write-ahead) mode.
			// SQLite will return BUSY if the database is locked by another connection.
			// We treat this as a transient database conflict, and the caller should retry.
			return WrapErr(DBConflict, "SQLiteStore: db-conflict", err)
		}
	}
	return WrapErr(DBProblem, fmt.Sprintf("SQLiteStore: db-problem: %s", where), err)
}

// STORE INTERFACE

func (s SQLiteStoreCtx) SetMaster(s1 []byte, s2 []byte, enc []byte) error {
	log.Printf("storing: %v %v %v", hex.EncodeToString(s1), hex.EncodeToString(s2), hex.EncodeToString(enc))
	return s.doTxn("SetMaster", func(tx *sql.Tx) error {
		res, err := tx.Exec("UPDATE config SET s1=?,s2=?,enc=?", s1, s2, enc)
		if err != nil {
			return err
		}
		num, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if num == 0 {
			_, err = tx.Exec("INSERT INTO config (s1,s2,enc) VALUES (?,?,?)", s1, s2, enc)
		}
		return err
	})
}

func (s SQLiteStoreCtx) GetMaster() (salt []byte, nonce []byte, encrypted []byte, err error) {
	return nil, nil, nil, nil
}
