package keymgr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"log"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/gossip/dnet"
	"github.com/dogeorg/doge/bip39"
	"github.com/dogeorg/doge/wrapped"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var _ internal.KeyMgr = &keyMgr{}

type keyMgr struct {
	store internal.StoreCtx
}

func New(store internal.StoreCtx) internal.KeyMgr {
	return &keyMgr{store: store}
}

var ErrOutOfEntropy, wrapOutOfEntropy = wrapped.New("not enough entropy available in the OS entropy pool")

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
	log.Printf("generated: %v", hex.EncodeToString(nodeKey.Pub))

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

func memZero(to []byte) {
	for i := range to {
		to[i] = 0
	}
}
