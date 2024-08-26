package internal

type KeyMgr interface {
	CreateKey(pass string) (mnemonic []string, err error)
}
