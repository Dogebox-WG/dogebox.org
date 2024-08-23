package internal

import (
	"context"
)

// Store is the top-level interface (e.g. SQLiteStore)
type Store interface {
	WithCtx(ctx context.Context) StoreCtx
}

// StoreCtx is a Store bound to a cancellable Context
type StoreCtx interface {
	SetMaster(s1 []byte, s2 []byte, enc []byte) error
	GetMaster() (s1 []byte, s2 []byte, enc []byte, err error)
}
