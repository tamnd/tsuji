// Package server wires the HTTP surface of tsuji: the OpenAI-compatible
// gateway under /api/v1, the management API, and the embedded web UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tamnd/tsuji/pkg/catalog"
	"github.com/tamnd/tsuji/pkg/config"
	"github.com/tamnd/tsuji/pkg/gateway"
	"github.com/tamnd/tsuji/pkg/route"
	"github.com/tamnd/tsuji/pkg/store"
)

// Server is the tsuji HTTP server.
type Server struct {
	cfg   *config.Config
	store *store.Store
	http  *http.Server
}

// New opens the store and builds the routing table.
func New(cfg *config.Config) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load()
	if err != nil {
		st.Close()
		return nil, err
	}

	s := &Server{cfg: cfg, store: st}
	gw := &gateway.Handler{
		Store:   st,
		Catalog: cat,
		Dialer:  route.New(cfg),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/v1/chat/completions", gw.ChatCompletions)
	mux.HandleFunc("GET /api/v1/models", gw.Models)
	mux.HandleFunc("GET /api/v1/generation", gw.Generation)
	mux.HandleFunc("GET /api/v1/key", gw.KeyInfo)

	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// ListenAndServe runs until the context is canceled, then shuts down cleanly.
func (s *Server) ListenAndServe(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- s.http.ListenAndServe() }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		return s.store.Close()
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
