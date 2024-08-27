package keymgr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/gossip/dnet"
	"github.com/dogeorg/doge/bip39"
	"github.com/dogeorg/doge/wrapped"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var _ internal.KeyMgr = &keyMgr{}

const SessionTime = 10 * 60 // seconds
const HandoverTime = 10     // seconds

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

var ErrOutOfEntropy, wrapOutOfEntropy = wrapped.New("not enough entropy available in the OS entropy pool")
var ErrWrongPassword = errors.New("incorrect password")
var ErrBadToken = errors.New("invalid or expired token")

func (km *keyMgr) CreateKey(pass string) (mnemonic []string, err error) {
	// generate salts
	salt := [16]byte{}
	_, err = rand.Read(salt[:])
	if err != nil {
		return nil, wrapOutOfEntropy(err)
	}
	nonce := [chacha20poly1305.NonceSizeX]byte{}
	_, err = rand.Read(nonce[:])
	if err != nil {
		return nil, wrapOutOfEntropy(err)
	}

	// generate mnemonic phrase
	mnemonic, seed, err := bip39.GenerateRandomMnemonic(256, pass, bip39.EnglishWordList)
	if err != nil {
		return nil, err
	}

	// ensure mnemonic phrase can be used later
	seed2, err := bip39.SeedFromMnemonic(mnemonic, pass, bip39.EnglishWordList)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(seed, seed2) {
		memZero(seed)
		memZero(seed2)
		return nil, err
		//sendError(w, http.StatusServiceUnavailable, "error", "bug: generated mnemonic did not round-trip", options)
	}
	memZero(seed2)

	// verify the generated seed produces a valid key
	nodeKey := dnet.KeyPairFromSeed(seed[0:32])
	memZero(seed)

	// encrypt the private key with the password key
	pwdKey := argon2.IDKey([]byte(pass), salt[:], 1, 64*1024, 4, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(pwdKey[:])
	memZero(pwdKey)
	if err != nil {
		return nil, err
	}
	encrypted := make([]byte, 0, len(nodeKey.Priv))
	encrypted = aead.Seal(encrypted, nonce[:], nodeKey.Priv, nil)
	memZero(nodeKey.Priv)
	memZero(nodeKey.Pub)

	// store the password nonce, master-key nonce, encrypted master-key
	err = km.store.SetMaster(salt[:], nonce[:], encrypted)
	memZero(encrypted)
	memZero(nonce[:])
	memZero(salt[:])
	if err != nil {
		return nil, err
	}

	return mnemonic, nil
}

func (km *keyMgr) Auth(pass string) (token string, ends int, err error) {
	salt, nonce, enc, err := km.store.GetMaster()
	if err != nil {
		return
	}
	pwdKey := argon2.IDKey([]byte(pass), salt, 1, 64*1024, 4, chacha20poly1305.KeySize)
	memZero(salt)
	aead, err := chacha20poly1305.NewX(pwdKey[:])
	if err != nil {
		return
	}
	memZero(pwdKey)
	var nodeKey dnet.KeyPair
	decrypted := make([]byte, 0, len(nodeKey.Priv))
	decrypted, err = aead.Open(decrypted, nonce, enc, nil)
	memZero(nonce)
	memZero(enc)
	if err != nil {
		// only errOpen "message authentication failed"
		return "", 0, ErrWrongPassword
	}
	memZero(decrypted)
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

func memZero(to []byte) {
	for i := range to {
		to[i] = 0
	}
}
