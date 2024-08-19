package web

import (
	"context"
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
	if r.Method == http.MethodPost {
		// request
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}

		// response
		res := CreateResponse{}
		sendJson(w, res, "POST, OPTIONS")
	} else {
		options(w, r, "POST, OPTIONS")
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
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}
		var args LoginRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			http.Error(w, fmt.Sprintf("error decoding JSON: %s", err.Error()), http.StatusBadRequest)
			return
		}

		// response
		res := LoginResponse{}
		sendJson(w, res, "POST, OPTIONS")
	} else {
		options(w, r, "POST, OPTIONS")
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
	if r.Method == http.MethodPost {
		// request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}
		var args ChangePassRequest
		err = json.Unmarshal(body, &args)
		if err != nil {
			http.Error(w, fmt.Sprintf("error decoding JSON: %s", err.Error()), http.StatusBadRequest)
			return
		}

		// response
		res := ChangePassResponse{}
		sendJson(w, res, "POST, OPTIONS")
	} else {
		options(w, r, "POST, OPTIONS")
	}
}

func sendJson(w http.ResponseWriter, res any, allow string) {
	bytes, err := json.Marshal(res)
	if err != nil {
		http.Error(w, fmt.Sprintf("error encoding JSON: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", allow)
	w.Write(bytes)
}

func options(w http.ResponseWriter, r *http.Request, options string) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Allow", options)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", options)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
