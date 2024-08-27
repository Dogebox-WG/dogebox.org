package keymgr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/gossip/dnet"
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
	err = km.encryptKey(MainKey, seed, pub, pass, false)
	memZero(seed)
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
	key, _, err := km.decryptKey(MainKey, pass)
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
	key, pub, err := km.decryptKey(MainKey, password)
	if err != nil {
		memZero(key)
		if errors.Is(err, internal.ErrNotFound) {
			return ErrNoKey
		}
		return err
	}
	err = km.encryptKey(MainKey, key, pub, newpass, true)
	memZero(key)
	if err != nil {
		return err
	}
	return nil
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
	err = km.encryptKey(MainKey, seed, pub, newpass, true)
	memZero(seed)
	if err != nil {
		return err
	}
	return nil
}

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

func (km *keyMgr) decryptKey(keyID int, pass string) (seed []byte, pub []byte, err error) {
	salt, nonce, enc, pub, err := km.store.GetKey(keyID)
	if err != nil {
		return nil, nil, err
	}
	// decrypt the private key using the password (via Argon2)
	pwdKey := argon2.IDKey([]byte(pass), salt, ArgonTime, ArgonMemory, ArgonThreads, chacha20poly1305.KeySize)
	memZero(salt)
	aead, err := chacha20poly1305.NewX(pwdKey[:])
	if err != nil {
		return nil, nil, err
	}
	memZero(pwdKey)
	var nodeKey dnet.KeyPair
	decrypted := make([]byte, 0, len(nodeKey.Priv))
	decrypted, err = aead.Open(decrypted, nonce, enc, nil)
	memZero(nonce)
	memZero(enc)
	if err != nil {
		// only errOpen "message authentication failed"
		return nil, nil, ErrWrongPassword
	}
	return decrypted, pub, nil
}

func (km *keyMgr) encryptKey(keyID int, seed []byte, pub []byte, pass string, allowReplace bool) error {
	// generate salts
	salt := [16]byte{}
	_, err := rand.Read(salt[:])
	if err != nil {
		return ErrOutOfEntropy
	}
	nonce := [chacha20poly1305.NonceSizeX]byte{}
	_, err = rand.Read(nonce[:])
	if err != nil {
		return ErrOutOfEntropy
	}
	return km.encryptKeyWithSalts(keyID, salt[:], nonce[:], seed, pub, pass, allowReplace)
}

func (km *keyMgr) encryptKeyWithSalts(keyID int, salt []byte, nonce []byte, seed []byte, pub []byte, pass string, allowReplace bool) error {
	// encrypt the private key with the password (via Argon2)
	pwdKey := argon2.IDKey([]byte(pass), salt, ArgonTime, ArgonMemory, ArgonThreads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(pwdKey)
	memZero(pwdKey) // minimum exposure time
	if err != nil {
		return err
	}
	encrypted := make([]byte, 0, len(seed))
	encrypted = aead.Seal(encrypted, nonce, seed, nil)

	// store the password nonce, key nonce, encrypted key
	err = km.store.SetKey(keyID, salt, nonce, encrypted, pub, allowReplace)
	// after storing, clearing buffers is critical
	memZero(encrypted)
	memZero(nonce)
	memZero(salt)
	return err
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
