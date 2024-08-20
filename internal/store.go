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
	SetMaster(salt []byte, nonce []byte, encrypted []byte) error
	GetMaster() (salt []byte, nonce []byte, encrypted []byte, err error)
}
