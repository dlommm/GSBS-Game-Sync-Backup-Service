package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
)

// Handler is the HTTP API handler.
type Handler struct {
	store store.Store
	auth  *auth.Service

	manifestCache struct {
		mu    sync.RWMutex
		entries []types.GameSaveLocation
		at     time.Time
	}
}

const manifestCacheTTL = 10 * time.Minute

func NewHandler(st store.Store, authSvc *auth.Service) *Handler {
	return &Handler{store: st, auth: authSvc}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
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
	default:
		http.NotFound(w, r)
	}
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
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
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
		userID, _, err := h.auth.ValidateToken(r.Context(), token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		fn(w, r, userID)
	}
}

// pullSaveItem is the JSON shape for one save (content as base64).
type pullSaveItem struct {
	GameID   string `json:"game_id"`
	PathKey  string `json:"path_key"`
	UpdatedAt string `json:"updated_at"`
	Content  string `json:"content"` // base64
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
	gameID := r.Header.Get("X-Game-ID")
	pathKey := r.Header.Get("X-Path-Key")
	filePath := r.Header.Get("X-File-Path")
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Game-ID and X-Path-Key required"})
		return
	}
	content, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
		return
	}
	_ = filePath
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
