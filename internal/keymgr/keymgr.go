package keymgr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"code.dogecoin.org/dkm/internal"
	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/bip39"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var _ internal.KeyMgr = &keyMgr{}

const SessionTime = 10 * 60 // seconds
const HandoverTime = 10     // seconds
const MainKey = 1           // ID of main key
const ArgonTime = 1
const ArgonMemory = 64 * 1024
const ArgonThreads = 4
const PrivateKeySize = 64
const MnemonicEntropyBits = 256

var ErrOutOfEntropy = errors.New("insufficient entropy available")
var ErrWrongPassword = errors.New("incorrect password")
var ErrBadToken = errors.New("invalid or expired token")
var ErrKeyExists = errors.New("key already exists")
var ErrTooManyAttempts = errors.New("too many attempts to generate a key")
var ErrWrongMnemonic = errors.New("mnemonic does not match existing key")
var ErrNoKey = errors.New("key has not been created")
var ErrWrongToken = errors.New("invalid token")

type keyMgr struct {
	store    internal.StoreCtx
	sessions map[string]session
}

type session struct {
	expires time.Time
	rolled  bool
}

func New(store internal.StoreCtx) internal.KeyMgr {
	return &keyMgr{
		store:    store,
		sessions: make(map[string]session),
	}
}

func (km *keyMgr) CreateKey(pass string) (mnemonic []string, err error) {
	mnemonic, seed, pub, err := km.generateMnemonic()
	if err != nil {
		return nil, err
	}
	err = km.encryptAndSetKey(MainKey, seed, pub, pass, false)
	if err != nil {
		if internal.IsAlreadyExistsError(err) {
			return nil, ErrKeyExists
		}
		return nil, err
	}
	return mnemonic, nil
}

func (km *keyMgr) LogIn(pass string) (token string, ends int, err error) {
	// verify the password
	key, _, err := km.getAndDecryptKey(MainKey, pass)
	memZero(key)
	if err != nil {
		if errors.Is(err, internal.ErrNotFound) {
			return "", 0, ErrNoKey
		}
		return // wrong password
	}
	token, ends, err = km.newSession()
	if err != nil {
		return
	}
	return token, ends, nil
}

func (km *keyMgr) RollToken(token string) (newtoken string, ends int, err error) {
	now := time.Now()
	if s, ok := km.sessions[token]; ok && !s.rolled && s.expires.After(now) {
		// keep the current token alive for a short handover time,
		// in case there are concurrent requests using the old token.
		km.sessions[token] = session{expires: time.Now().Add(HandoverTime * time.Second), rolled: true}
		// issue a new token.
		return km.newSession()
	} else {
		// the token has already expired.
		delete(km.sessions, token)
		return "", 0, ErrBadToken
	}
}

func (km *keyMgr) LogOut(token string) {
	// invalidate the token if it exists.
	delete(km.sessions, token)
}

func (km *keyMgr) ChangePassword(password string, newpass string) error {
	// decrypt the key using the current password
	key, pub, err := km.getAndDecryptKey(MainKey, password)
	if err != nil {
		memZero(key)
		if errors.Is(err, internal.ErrNotFound) {
			return ErrNoKey
		}
		return err
	}
	return km.encryptAndSetKey(MainKey, key, pub, newpass, true)
}

func (km *keyMgr) RecoverPassword(mnemonic []string, newpass string) error {
	// get the existing stored pubkey
	pub, err := km.store.GetKeyPub(MainKey)
	if err != nil {
		if errors.Is(err, internal.ErrNotFound) {
			return ErrNoKey
		}
		return err
	}

	// re-generate the key from the supplied mnemonic
	seed, err := bip39.SeedFromMnemonic(mnemonic, "", bip39.EnglishWordList)
	if err != nil {
		return err // bad mnemonic
	}
	if !doge.ECKeyIsValid(seed[0:32]) {
		return ErrWrongMnemonic // we check validity when we generate the mnemonic
	}
	newpub := doge.ECPubKeyFromECPrivKey(seed[0:32])
	if !bytes.Equal(pub, newpub) {
		return ErrWrongMnemonic // mnemonic pubkey differs from the stored pubkey
	}

	// re-encrypt the stored key using the new password
	return km.encryptAndSetKey(MainKey, seed, pub, newpass, true)
}

func (km *keyMgr) GetPubKey(id string) (pubkey []byte, err error) {
	return km.store.GetDelegatePub(id)
}

func (km *keyMgr) GetPrivKey(id string, token string) (privkey, pubkey []byte, err error) {
	salt, nonce, enc, pub, err := km.store.GetDelegatePriv(id)
	if err != nil {
		return nil, nil, err
	}
	priv, err := km.decryptKey(salt, nonce, enc, token)
	memZero(enc)
	memZero(salt)
	memZero(nonce)
	if err != nil {
		memZero(priv)
		if errors.Is(err, ErrWrongPassword) { // from decryptKey
			err = ErrWrongToken
		}
		return nil, nil, err
	}
	return priv, pub, nil
}

func (km *keyMgr) DelegateKey(id string) (token string, pubkey []byte, err error) {
	// XXX yet to do
	return "", nil, ErrNoKey
}

// HELPERS

func (km *keyMgr) newSession() (token string, ends int, err error) {
	// clean out expired tokens.
	now := time.Now()
	for key, s := range km.sessions {
		if s.expires.Before(now) {
			// seems safe: https://go.dev/doc/effective_go#for
			delete(km.sessions, key)
		}
	}
	// generate a cryptographically-secure random token.
	tok := [16]byte{}
	_, err = rand.Read(tok[:])
	if err != nil {
		return "", 0, ErrOutOfEntropy
	}
	token = hex.EncodeToString(tok[:])
	km.sessions[token] = session{expires: time.Now().Add((SessionTime + HandoverTime) * time.Second)}
	return token, SessionTime, nil
}

func (km *keyMgr) getAndDecryptKey(keyId int, pass string) (key []byte, pub []byte, err error) {
	salt, nonce, enc, pubk, err := km.store.GetKey(keyId)
	if err != nil {
		return nil, nil, err
	}
	dec, err := km.decryptKey(salt, nonce, enc, pass)
	memZero(enc)
	memZero(salt)
	memZero(nonce)
	return dec, pubk, err
}

func (km *keyMgr) decryptKey(salt []byte, nonce []byte, enc []byte, pass string) (key []byte, err error) {
	// decrypt the private key using the password (via Argon2)
	pwdKey := argon2.IDKey([]byte(pass), salt, ArgonTime, ArgonMemory, ArgonThreads, chacha20poly1305.KeySize)
	memZero(salt)
	aead, err := chacha20poly1305.NewX(pwdKey[:])
	memZero(pwdKey)
	if err != nil {
		memZero(nonce)
		memZero(enc)
		return nil, err
	}
	key = make([]byte, 0, PrivateKeySize)
	key, err = aead.Open(key, nonce, enc, nil)
	memZero(nonce)
	memZero(enc)
	if err != nil {
		// only errOpen "message authentication failed"
		return nil, ErrWrongPassword
	}
	return key, nil
}

// encrypt secret with password and store. clears secret.
func (km *keyMgr) encryptAndSetKey(keyId int, secret, pub []byte, pass string, allowReplace bool) (err error) {
	salt, nonce, enc, err := km.encryptKey(secret, pass)
	memZero(secret)
	if err != nil {
		return err
	}
	// store the password nonce, key nonce, encrypted key
	err = km.store.SetKey(keyId, salt, nonce, enc, pub, allowReplace)
	memZero(enc)
	memZero(nonce)
	memZero(salt)
	return err
}

// encrypt secret with password.
func (km *keyMgr) encryptKey(secret []byte, pass string) (salt, nonce, enc []byte, err error) {
	// generate salts
	salt = make([]byte, 16)
	_, err = rand.Read(salt)
	if err != nil {
		return nil, nil, nil, ErrOutOfEntropy
	}
	nonce = make([]byte, chacha20poly1305.NonceSizeX)
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, nil, nil, ErrOutOfEntropy
	}

	// encrypt the private key with the password (via Argon2)
	pwdKey := argon2.IDKey([]byte(pass), salt, ArgonTime, ArgonMemory, ArgonThreads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(pwdKey)
	memZero(pwdKey) // minimum exposure time
	if err != nil {
		return nil, nil, nil, err
	}
	// seed is always PrivateKeySize, encrypted is typically 80 bytes
	enc = make([]byte, 0, 2*PrivateKeySize) // to avoid realloc
	enc = aead.Seal(enc, nonce, secret, nil)

	return salt, nonce, enc, err
}

func (km *keyMgr) generateMnemonic() (mnemonic []string, seed []byte, pub []byte, err error) {
	attempt := 0
	for attempt < 1000 {
		// cannot use the password as BIP39 passphrase here,
		// otherwise we cannot support password recovery.
		mnemonic, seed, err = bip39.GenerateRandomMnemonic(MnemonicEntropyBits, "", bip39.EnglishWordList)
		if err != nil {
			return // only ErrOutOfEntropy
		}

		// ensure mnemonic phrase can be used later
		seed2, err := bip39.SeedFromMnemonic(mnemonic, "", bip39.EnglishWordList)
		if err != nil {
			log.Printf("BUG: could not decode generated mnemonic: %v", mnemonic)
			attempt += 1
			continue
		}
		if !bytes.Equal(seed, seed2) {
			log.Printf("BUG: mnemonic did not round-trip: %v", mnemonic)
			attempt += 1
			continue
		}
		memZero(seed2)

		// verify the generated seed represents a valid private key
		if !doge.ECKeyIsValid(seed[0:32]) {
			log.Printf("BUG: mnemonic generates an invalid private key: %v", mnemonic)
			attempt += 1
			continue
		}
		pub = doge.ECPubKeyFromECPrivKey(seed[0:32])
		return mnemonic, seed, pub, nil
	}
	return nil, nil, nil, ErrTooManyAttempts
}

func memZero(to []byte) {
	for i := range to {
		to[i] = 0
	}
}
