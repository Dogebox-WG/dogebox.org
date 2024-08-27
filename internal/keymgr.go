package internal

type KeyMgr interface {
	CreateKey(pass string) (mnemonic []string, err error)
	LogIn(pass string) (token string, ends int, err error)
	RollToken(token string) (newtoken string, ends int, err error)
	LogOut(token string)
	ChangePassword(password string, newpass string) error
	RecoverPassword(mnemonic []string, newpass string) error
}
