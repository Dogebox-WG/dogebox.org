package web

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"code.dogecoin.org/dkm/internal/spec"
	"code.dogecoin.org/gossip/dnet"
	"code.dogecoin.org/governor"
)

type WebAPI struct {
	governor.ServiceCtx
	store  spec.Store
	cstore spec.StoreCtx
	srv    http.Server
}

func New(bind dnet.Address, store spec.Store) governor.Service {
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
	mux.HandleFunc("/change-password", a.changePassword)

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

type CreateResponse struct {
	Words []string `json:"words"`
}

func (a *WebAPI) create(w http.ResponseWriter, r *http.Request) {
	options := "POST, OPTIONS"
	if r.Method == http.MethodPost {
		// request
		_, err := io.ReadAll(r.Body)
		if err != nil {
			sendError(w, http.StatusBadRequest, "bad-request", fmt.Sprintf("bad request: %v", err), options)
			return
		}

		// generate the new key
		nodeKey, err := dnet.GenerateKeyPair()
		if err != nil {
			sendError(w, http.StatusFailedDependency, "bad-key", fmt.Sprintf("cannot generate key: %v", err), options)
		}

		// response
		res := CreateResponse{Words: []string{hex.EncodeToString(nodeKey.Pub)}}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type LoginRequest struct {
	Phrase string `json:"phrase"`
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
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("deconding JSON: %s", err.Error()), options)
			return
		}

		// response
		res := LoginResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

type ChangePassRequest struct {
	Phrase    string `json:"phrase"`
	NewPhrase string `json:"newphrase"`
}
type ChangePassResponse struct {
	Valid bool   `json:"valid"`
	Token string `json:"token"`
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
			sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("deconding JSON: %s", err.Error()), options)
			return
		}

		// response
		res := ChangePassResponse{}
		sendJson(w, res, options)
	} else {
		sendOptions(w, r, options)
	}
}

func sendJson(w http.ResponseWriter, res any, options string) {
	bytes, err := json.Marshal(res)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "json", fmt.Sprintf("encoding JSON: %s", err.Error()), options)
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
