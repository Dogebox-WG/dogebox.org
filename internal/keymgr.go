package internal

type KeyMgr interface {
	CreateKey(pass string) (mnemonic []string, err error)
	Auth(pass string) (valid bool, token string, ends int, err error)
	RollToken(token string) (newtoken string, ends int, err error)
	LogOut(token string)
}
