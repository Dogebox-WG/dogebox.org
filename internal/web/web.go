package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/gossip/dnet"
	"code.dogecoin.org/governor"
	"github.com/dogeorg/doge/bip39"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

type WebAPI struct {
	governor.ServiceCtx
	store  internal.Store
	cstore internal.StoreCtx
	srv    http.Server
}

func New(bind dnet.Address, store internal.Store) governor.Service {
	mux := http.NewServeMux()
	a := &WebAPI{
		store: store,
		srv: http.Server{
			Addr:    bind.String(),
			Handler: mux,
		},
	}
	mux.HandleFunc("/create", a.create)
	mux.HandleFunc("/login", a.login)
	mux.HandleFunc("/exchange-token", a.exchangeToken)
	mux.HandleFunc("/change-password", a.changePassword)
	mux.HandleFunc("/recover-password", a.recoverPassword)

	return a
}

// goroutine
func (a *WebAPI) Run() {
	a.cstore = a.store.WithCtx(a.Context) // Service Context is first available here
	log.Printf("[dkm] listening on: %v", a.srv.Addr)
	if err := a.srv.ListenAndServe(); err != http.ErrServerClosed { // blocking call
		log.Printf("[dkm] HTTP server: %v", err)
	}
}

// called on any
func (a *WebAPI) Stop() {
	// new goroutine because Shutdown() blocks
	go func() {
		// cannot use ServiceCtx here because it's already cancelled
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.srv.Shutdown(ctx) // blocking call
		cancel()
	}()
}

// WEB API

type CreateRequest struct {
	Phrase string `json:"password"`
}
type CreateResponse struct {
	Seedphrase []string `json:"seedphrase"`
}

func (a *WebAPI) create(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args CreateRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// generate salts
		salt := [16]byte{}
		_, err = rand.Read(salt[:])
		if err != nil {
			sendError(w, http.StatusServiceUnavailable, "entropy", fmt.Sprintf("insufficient entropy: %v", err), options)
		}
		nonce := [chacha20poly1305.NonceSizeX]byte{}
		_, err = rand.Read(nonce[:])
		if err != nil {
			sendError(w, http.StatusServiceUnavailable, "entropy", fmt.Sprintf("insufficient entropy: %v", err), options)
		}

		// generate mnemonic phrase
		mnemonic, seed, err := bip39.GenerateRandomMnemonic(256, args.Phrase, bip39.EnglishWordList)
		if err != nil {
			sendError(w, http.StatusServiceUnavailable, codeForErr(err), err.Error(), options)
		}

		// ensure mnemonic phrase can be used later
		seed2, err := bip39.SeedFromMnemonic(mnemonic, args.Phrase, bip39.EnglishWordList)
		if err != nil {
			sendError(w, http.StatusServiceUnavailable, codeForErr(err), err.Error(), options)
		}
		if !bytes.Equal(seed, seed2) {
			memZero(seed)
			memZero(seed2)
			sendError(w, http.StatusServiceUnavailable, "error", "bug: generated mnemonic did not round-trip", options)
		}
		memZero(seed2)

		// verify the generated seed produces a valid key
		nodeKey := dnet.KeyPairFromSeed(seed[0:32])
		memZero(seed)
		log.Printf("generated: %v", hex.EncodeToString(nodeKey.Pub))

		// encrypt the private key with the password key
		pwdKey := argon2.IDKey([]byte(args.Phrase), salt[:], 1, 64*1024, 4, chacha20poly1305.KeySize)
		aead, err := chacha20poly1305.NewX(pwdKey[:])
		memZero(pwdKey)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "encrypt", fmt.Sprintf("cannot encrypt key: %v", err), options)
		}
		encrypted := make([]byte, 0, len(nodeKey.Priv))
		encrypted = aead.Seal(encrypted, nonce[:], nodeKey.Priv, nil)
		memZero(nodeKey.Priv)
		memZero(nodeKey.Pub)

		// store the password nonce, master-key nonce, encrypted master-key
		err = a.cstore.SetMaster(salt[:], nonce[:], encrypted)
		memZero(encrypted)
		memZero(nonce[:])
		memZero(salt[:])
		if err != nil {
			sendError(w, http.StatusInternalServerError, "bad-key", fmt.Sprintf("cannot generate key: %v", err), options)
		}

		// response
		res := CreateResponse{Seedphrase: mnemonic}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

func memZero(to []byte) {
	for i := range to {
		to[i] = 0
	}
}

type LoginRequest struct {
	Password string `json:"password"`
}
type LoginResponse struct {
	Valid bool   `json:"valid"`
	Token string `json:"token"`
}

func (a *WebAPI) login(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args LoginRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// response
		res := LoginResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type ExchangeTokenRequest struct {
	Token string `json:"token"`
}
type ExchangeTokenResponse struct {
	Valid    bool   `json:"valid"`
	NewToken string `json:"new_token"`
}

func (a *WebAPI) exchangeToken(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args ExchangeTokenRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// response
		res := ExchangeTokenResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type ChangePassRequest struct {
	Password    string `json:"password"`
	NewPassword string `json:"new_password"`
}
type ChangePassResponse struct {
	Changed bool `json:"valid"`
}

func (a *WebAPI) changePassword(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args ChangePassRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// response
		res := ChangePassResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type RecoveryRequest struct {
	Seedphrase  []string `json:"seedphrase"`
	NewPassword string   `json:"new_password"`
}
type RecoveryResponse struct {
	Changed bool `json:"valid"`
}

func (a *WebAPI) recoverPassword(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args RecoveryRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		// response
		res := RecoveryResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

// HELPERS

func sendJson(w http.ResponseWriter, res any, options string) {
	bytes, err := json.Marshal(res)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("encoding JSON: %v", err), options)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.Write(bytes)
}

type WebError struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func sendError(w http.ResponseWriter, statusCode int, code string, reason string, options string) {
	bytes, err := json.Marshal(WebError{Error: code, Reason: reason})
	if err != nil {
		bytes = []byte(fmt.Sprintf("{\"error\":\"json\",\"reason\":\"encoding JSON: %s\"}", err.Error()))
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.WriteHeader(statusCode)
	w.Write(bytes)
}

func sendOptions(w http.ResponseWriter, r *http.Request, options string) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Allow", options)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", options)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func codeForErr(err error) string {
	if errors.Is(err, bip39.ErrBadEntropy) {
		return "range"
	}
	if errors.Is(err, bip39.ErrOutOfEntropy) {
		return "entropy"
	}
	if errors.Is(err, bip39.ErrWrongWord) {
		return "wordlist"
	}
	if errors.Is(err, bip39.ErrWrongChecksum) {
		return "checksum"
	}
	if errors.Is(err, bip39.ErrWrongLength) {
		return "length"
	}
	return "error"
}
