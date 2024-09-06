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
	enc BLOB NOT NULL,
	pub BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS delegate (
	id TEXT PRIMARY KEY,
	s1 BLOB NOT NULL,
	s2 BLOB NOT NULL,
	enc BLOB NOT NULL,
	pub BLOB NOT NULL,
	keyid INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS delegate_keyid_i ON delegate (keyid);
`

type SQLiteStore struct {
	db *sql.DB
}

type SQLiteStoreCtx struct {
	_db *sql.DB
	db  Queryable
	ctx context.Context
	tx  *sql.Tx // set if inside a transaction, otherwise nil
}

// The common read-only parts of sql.DB and sql.Tx interfaces, so we can pass either
// one to some helper functions (for methods that appear on both SQLiteStore and
// SQLiteStoreTransaction)
type Queryable interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
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
		db:  s.db,
		ctx: ctx,
	}
}

func IsConflict(err error) bool {
	// this allows the work() function in doTxn() to return ErrDBConflict
	// to cause the transaction to retry.
	if errors.Is(err, internal.ErrDBConflict) {
		return true
	}
	// these errors come from db.Begin(), db.Commit() or potentially any query.
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			return true
		}
	}
	return false
}

func IsConstraint(err error) bool {
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrConstraint {
			return true
		}
	}
	return false
}

func (s SQLiteStoreCtx) doTxn(name string, work func(tx *sql.Tx) error) error {
	db := s._db
	if s.tx != nil {
		// already running inside a user-level store.Transaction,
		// so just run the work function directly.
		return work(s.tx)
	}
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
		// work() may return ErrDBConflict to retry the transaction.
		// any sqlite conflict error will also retry the transaction.
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
	if errors.Is(err, sql.ErrNoRows) {
		return internal.ErrNotFound
	}
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrConstraint {
			// MUST detect 'AlreadyExists' to fulfil the API contract!
			// Constraint violation, e.g. a duplicate key.
			return internal.ErrAlreadyExists
		}
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			// SQLite has a single-writer policy, even in WAL (write-ahead) mode.
			// SQLite will return BUSY if the database is locked by another connection.
			// We treat this as a transient database conflict, and the caller should retry.
			return internal.ErrDBConflict
		}
	}
	return fmt.Errorf("store: %v: %w", where, err)
}

// STORE INTERFACE

func (s SQLiteStoreCtx) Transaction(work func(tx internal.StoreTxn) error) error {
	return s.doTxn("txn", func(tx *sql.Tx) error {
		stx := &SQLiteStoreCtx{
			_db: s._db,
			db:  tx,
			ctx: s.ctx,
			tx:  tx,
		}
		return work(stx)
	})
}

func (s SQLiteStoreCtx) SetKey(id int, s1 []byte, s2 []byte, enc []byte, pub []byte, allowReplace bool) error {
	return s.doTxn("SetKey", func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO config (id,s1,s2,enc,pub) VALUES (?,?,?,?,?)", id, s1, s2, enc, pub)
		if err != nil {
			if IsConstraint(err) && allowReplace {
				// already exists and allowed to replace.
				_, err = tx.Exec("UPDATE config SET s1=?,s2=?,enc=?,pub=? WHERE id=?", s1, s2, enc, pub, id)
				if err != nil {
					return dbErr(err, "SetKey")
				}
				return nil
			}
			return dbErr(err, "SetKey") // AlreadyExists or error
		}
		return nil
	})
}

func (s SQLiteStoreCtx) GetKey(id int) (s1 []byte, s2 []byte, enc []byte, pub []byte, err error) {
	err = s.doTxn("GetKey", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT s1,s2,enc,pub FROM config WHERE id=?", id)
		err = row.Scan(&s1, &s2, &enc, &pub)
		if err != nil {
			return dbErr(err, "GetKey")
		}
		return nil
	})
	return
}

func (s SQLiteStoreCtx) GetKeyPub(id int) (pub []byte, err error) {
	err = s.doTxn("GetKeyPub", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT pub FROM config WHERE id=?", id)
		err = row.Scan(&pub)
		if err != nil {
			return dbErr(err, "GetKeyPub")
		}
		return nil
	})
	return
}

func (s SQLiteStoreCtx) SetDelegate(id string, s1, s2, enc, pub []byte, keyid uint32) (err error) {
	return s.doTxn("SetDelegate", func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO delegate (id,s1,s2,enc,pub,keyid) VALUES (?,?,?,?,?,?)", id, s1, s2, enc, pub, keyid)
		if err != nil {
			return dbErr(err, "SetDelegate") // AlreadyExists or error
		}
		return nil
	})
}

func (s SQLiteStoreCtx) GetDelegatePub(id string) (pub []byte, err error) {
	err = s.doTxn("GetDelegatePub", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT pub FROM delegate WHERE id=?", id)
		err = row.Scan(&pub)
		if err != nil {
			return dbErr(err, "GetDelegatePub")
		}
		return nil
	})
	return
}

func (s SQLiteStoreCtx) GetDelegatePriv(id string) (s1, s2, enc, pub []byte, err error) {
	err = s.doTxn("GetDelegatePriv", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT s1,s2,enc,pub FROM delegate WHERE id=?", id)
		err = row.Scan(&s1, &s2, &enc, &pub)
		if err != nil {
			return dbErr(err, "GetDelegatePriv")
		}
		return nil
	})
	return
}

func (s SQLiteStoreCtx) GetMaxDelegate() (max uint32, err error) {
	err = s.doTxn("GetMaxDelegate", func(tx *sql.Tx) error {
		row := tx.QueryRow("SELECT COALESCE(MAX(keyid),0) FROM delegate")
		err = row.Scan(&max)
		if err != nil {
			return dbErr(err, "GetMaxDelegate")
		}
		return nil
	})
	return
}
