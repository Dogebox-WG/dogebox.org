package keymgr

import "code.dogecoin.org/dkm/internal"

type keyMgr struct {
}

func New() internal.KeyMgr {
	return &keyMgr{}
}
