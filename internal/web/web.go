package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code.dogecoin.org/dkm/internal"
	"code.dogecoin.org/dkm/internal/keymgr"
	"code.dogecoin.org/gossip/dnet"
	"code.dogecoin.org/governor"
	"github.com/dogeorg/doge/bip39"
)

type WebAPI struct {
	governor.ServiceCtx
	store  internal.Store
	cstore internal.StoreCtx
	keymgr internal.KeyMgr
	srv    http.Server
}

func New(bind dnet.Address, store internal.Store, keymgr internal.KeyMgr) governor.Service {
	mux := http.NewServeMux()
	a := &WebAPI{
		store:  store,
		keymgr: keymgr,
		srv: http.Server{
			Addr:    bind.String(),
			Handler: mux,
		},
	}
	mux.HandleFunc("/create", a.create)
	mux.HandleFunc("/login", a.login)
	mux.HandleFunc("/roll-token", a.rollToken)
	mux.HandleFunc("/logout", a.logout)
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
	Password string `json:"password"`
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

		// validate password
		pass := strings.TrimSpace(args.Password)
		if len(pass) < 1 {
			sendError(w, http.StatusInternalServerError, "password", "password is empty", options)
			return
		}

		// generate the new key
		mnemonic, err := a.keymgr.CreateKey(pass)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := CreateResponse{Seedphrase: mnemonic}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type LoginRequest struct {
	Password string `json:"password"`
}
type LoginResponse struct {
	Valid    bool   `json:"valid"`
	Token    string `json:"token"`
	ValidFor int    `json:"valid_for"`
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

		// validate password
		pass := strings.TrimSpace(args.Password)
		if len(pass) < 1 {
			sendError(w, http.StatusInternalServerError, "password", "password is empty", options)
			return
		}

		valid, token, ends, err := a.keymgr.Auth(pass)
		if err != nil {
			sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
		}

		// response
		res := LoginResponse{Valid: valid, Token: token, ValidFor: ends}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type RollTokenRequest struct {
	Token string `json:"token"`
}
type RollTokenResponse struct {
	Valid    bool   `json:"valid"`
	NewToken string `json:"new_token"`
	ValidFor int    `json:"valid_for"`
}

func (a *WebAPI) rollToken(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args RollTokenRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		valid := true
		newtoken, ends, err := a.keymgr.RollToken(args.Token)
		if err != nil {
			if err == keymgr.ErrBadToken {
				valid = false
				err = nil
			} else {
				sendError(w, http.StatusInternalServerError, codeForErr(err), err.Error(), options)
				return
			}
		}

		// response
		res := RollTokenResponse{Valid: valid, NewToken: newtoken, ValidFor: ends}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type LogOutRequest struct {
	Token string `json:"token"`
}
type LogOutResponse struct {
}

func (a *WebAPI) logout(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}
		var args LogOutRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("decoding JSON: %v", err), options)
			return
		}

		a.keymgr.LogOut(args.Token)

		// response
		res := LogOutResponse{}
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
	if errors.Is(err, bip39.ErrOutOfEntropy) || errors.Is(err, keymgr.ErrOutOfEntropy) {
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
