package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

// MaxSaveSize is the maximum allowed body size for POST /api/saves (50 MiB).
const MaxSaveSize = 50 * 1024 * 1024

// Handler is the HTTP API handler.
type Handler struct {
	store         store.Store
	auth          *auth.Service
	allowRegister bool
	hub           *sse.Hub

	manifestCache struct {
		mu      sync.RWMutex
		entries []types.GameSaveLocation
		at      time.Time
	}
}

const manifestCacheTTL = 10 * time.Minute

func NewHandler(st store.Store, authSvc *auth.Service, allowRegister bool, hub *sse.Hub) *Handler {
	return &Handler{store: st, auth: authSvc, allowRegister: allowRegister, hub: hub}
}

// InvalidateManifestCache clears the in-memory manifest cache so the next
// request reads fresh data from the DB.
func (h *Handler) InvalidateManifestCache() {
	h.manifestCache.mu.Lock()
	h.manifestCache.entries = nil
	h.manifestCache.at = time.Time{}
	h.manifestCache.mu.Unlock()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/health" && r.Method == http.MethodGet:
		h.handleHealth(w, r)
	case r.URL.Path == "/api/register" && r.Method == http.MethodPost:
		h.handleRegister(w, r)
	case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
		h.handleLogin(w, r)
	case r.URL.Path == "/api/saves" && r.Method == http.MethodGet:
		h.withAuth(h.handlePull)(w, r)
	case r.URL.Path == "/api/saves" && r.Method == http.MethodPost:
		h.withAuth(h.handlePush)(w, r)
	case r.URL.Path == "/api/manifest" && r.Method == http.MethodGet:
		h.handleManifest(w, r)
	case r.URL.Path == "/api/events" && r.Method == http.MethodGet:
		h.withAuth(h.handleSSE)(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	ClientName string `json:"client_name"`
	ClientOS   string `json:"client_os"` // "windows" or "linux"
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegister {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "registration is disabled"})
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	if len(req.Username) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username too long"})
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken or error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	if req.ClientOS == "" {
		req.ClientOS = "unknown"
	}
	if req.ClientName == "" {
		req.ClientName = "client"
	}
	_, token, err := h.auth.Login(r.Context(), req.Username, req.Password, req.ClientName, req.ClientOS)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"})
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

// withAuth wraps a handler requiring auth. Passes userID to the handler.
// Also updates client last_seen on every authenticated request.
func (h *Handler) withAuth(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "" && len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
			return
		}
		userID, clientID, err := h.auth.ValidateToken(r.Context(), token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		// Update last_seen asynchronously (best-effort; use background context so it runs after response).
		clientIDCopy := clientID
		go func() {
			ctx := context.Background()
			if err := h.store.UpdateClientLastSeen(ctx, clientIDCopy); err != nil {
				log.Printf("update last_seen for client %s: %v", clientIDCopy, err)
			}
		}()
		fn(w, r, userID)
	}
}

// pullSaveItem is the JSON shape for one save (content as base64).
type pullSaveItem struct {
	GameID    string `json:"game_id"`
	PathKey   string `json:"path_key"`
	UpdatedAt string `json:"updated_at"`
	Content   string `json:"content"` // base64
}

func (h *Handler) handlePull(w http.ResponseWriter, r *http.Request, userID string) {
	saves, err := h.store.ListSaves(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	items := make([]pullSaveItem, len(saves))
	for i := range saves {
		items[i] = pullSaveItem{
			GameID:    saves[i].GameID,
			PathKey:   saves[i].PathKey,
			UpdatedAt: saves[i].UpdatedAt,
			Content:   encodeBase64(saves[i].Content),
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"saves": items})
}

func (h *Handler) handlePush(w http.ResponseWriter, r *http.Request, userID string) {
	gameID := strings.TrimSpace(r.Header.Get("X-Game-ID"))
	pathKey := strings.TrimSpace(r.Header.Get("X-Path-Key"))
	filePath := r.Header.Get("X-File-Path")
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Game-ID and X-Path-Key required"})
		return
	}
	if len(gameID) > 512 || len(pathKey) > 1024 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Game-ID or X-Path-Key too long"})
		return
	}
	limited := http.MaxBytesReader(nil, r.Body, MaxSaveSize)
	content, err := io.ReadAll(limited)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "save too large (max 50 MiB)"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
		return
	}
	if len(content) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty content"})
		return
	}
	log.Printf("push: user=%s game=%s path_key=%s file=%s size=%d", userID, gameID, pathKey, filePath, len(content))
	if err := h.store.UpsertSave(r.Context(), userID, gameID, pathKey, content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleManifest(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	var entries []types.GameSaveLocation
	var err error
	if since != "" {
		entries, err = h.store.GetManifestSince(r.Context(), since)
	} else {
		h.manifestCache.mu.RLock()
		if time.Since(h.manifestCache.at) < manifestCacheTTL && len(h.manifestCache.entries) > 0 {
			entries = h.manifestCache.entries
			h.manifestCache.mu.RUnlock()
		} else {
			h.manifestCache.mu.RUnlock()
			entries, err = h.store.ListGameSaveLocations(r.Context())
			if err == nil {
				h.manifestCache.mu.Lock()
				h.manifestCache.entries = entries
				h.manifestCache.at = time.Now()
				h.manifestCache.mu.Unlock()
			}
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "manifest failed"})
		return
	}

	// Log the fetch (best-effort). If auth token is present, resolve client info.
	go func() {
		ctx := context.Background()
		clientID, clientName, username := "", "", ""
		token := r.Header.Get("Authorization")
		if token != "" && len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != "" {
			if uid, cid, cname, _, authErr := h.store.ClientByToken(ctx, token); authErr == nil {
				clientID = cid
				clientName = cname
				if uname, err := h.store.UsernameByID(ctx, uid); err == nil {
					username = uname
				}
			}
		}
		_ = h.store.LogManifestFetch(ctx, clientID, clientName, username, len(entries))
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// handleSSE streams server-sent events to an authenticated client.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := h.hub.Subscribe(userID)
	defer unsub()

	// Send initial heartbeat so the client knows the connection is live.
	fmt.Fprint(w, ": heartbeat\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, evt.Format())
			flusher.Flush()
		}
	}
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
