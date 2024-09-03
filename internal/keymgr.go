package internal

type KeyMgr interface {
	CreateKey(pass string) (mnemonic []string, err error)
	LogIn(pass string) (token string, ends int, err error)
	RollToken(token string) (newtoken string, ends int, err error)
	LogOut(token string)
	ChangePassword(password string, newpass string) error
	RecoverPassword(mnemonic []string, newpass string) error
	GetPubKey(id string) (pubkey []byte, err error)
	GetPrivKey(id string, token string) (privkey []byte, pubkey []byte, err error)
	DelegateKey(id string) (token string, pubkey []byte, err error)
}
