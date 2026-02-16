package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/ratelimit"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/pquerna/otp/totp"
)

// MaxSaveSize is the maximum allowed body size for POST /api/saves (50 MiB).
const MaxSaveSize = 50 * 1024 * 1024

// Handler is the HTTP API handler.
type Handler struct {
	store            store.Store
	auth             *auth.Service
	allowRegister    bool
	hub              *sse.Hub
	authLimiter      *ratelimit.Limiter
	pushLimiter      *ratelimit.Limiter
	manifestLimiter  *ratelimit.Limiter
	maxStorageBytes  int64 // 0 = unlimited
	readOnly         bool  // if true, reject push and delete
	sessionSecret    string // for signing TOTP step token when 2FA enabled; empty = no API 2FA
	version          string // server version for health endpoint

	manifestCache struct {
		mu      sync.RWMutex
		entries []types.GameSaveLocation
		at      time.Time
	}
}

const manifestCacheTTL = 10 * time.Minute

// NewHandler creates an API handler. maxStorageBytes 0 = unlimited; readOnly blocks push/delete.
// sessionSecret is used to sign the TOTP step token when 2FA is enabled; pass the same value as WebUI session secret. Empty = no API 2FA.
// version is included in the health response when non-empty.
func NewHandler(st store.Store, authSvc *auth.Service, allowRegister bool, hub *sse.Hub, authLimiter, pushLimiter, manifestLimiter *ratelimit.Limiter, maxStorageBytes int64, readOnly bool, sessionSecret string, version string) *Handler {
	return &Handler{store: st, auth: authSvc, allowRegister: allowRegister, hub: hub, authLimiter: authLimiter, pushLimiter: pushLimiter, manifestLimiter: manifestLimiter, maxStorageBytes: maxStorageBytes, readOnly: readOnly, sessionSecret: sessionSecret, version: version}
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
	case r.URL.Path == "/api/login/totp" && r.Method == http.MethodPost:
		h.handleLoginTOTP(w, r)
	case r.URL.Path == "/api/saves" && r.Method == http.MethodGet:
		if r.URL.Query().Get("summaries") == "1" {
			h.withAuth(h.handleSaveSummaries)(w, r)
		} else {
			h.withAuth(h.handlePull)(w, r)
		}
	case r.URL.Path == "/api/saves" && r.Method == http.MethodPost:
		h.withAuth(h.handlePush)(w, r)
	case r.URL.Path == "/api/saves" && r.Method == http.MethodDelete:
		h.withAuth(h.handleDeleteSave)(w, r)
	case r.URL.Path == "/api/manifest" && r.Method == http.MethodGet:
		h.handleManifest(w, r)
	case r.URL.Path == "/api/clients" && r.Method == http.MethodGet:
		h.withAuth(h.handleListClients)(w, r)
	case r.URL.Path == "/api/saves/versions" && r.Method == http.MethodGet:
		h.withAuth(h.handleListSaveVersions)(w, r)
	case r.URL.Path == "/api/saves/versions/download" && r.Method == http.MethodGet:
		h.withAuth(h.handleGetSaveVersion)(w, r)
	case r.URL.Path == "/api/saves/versions/restore" && r.Method == http.MethodPost:
		h.withAuth(h.handleRestoreSaveVersion)(w, r)
	case r.URL.Path == "/api/events" && r.Method == http.MethodGet:
		h.withAuth(h.handleSSE)(w, r)
	case r.URL.Path == "/api/change-password" && r.Method == http.MethodPost:
		h.withAuth(h.handleChangePassword)(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Optional readiness: ?ready=1 checks DB connectivity for load balancers.
	if r.URL.Query().Get("ready") == "1" {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, err := h.store.CountUsers(ctx)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			writeHealthJSON(w, "unhealthy", h.version, "error")
			return
		}
		w.WriteHeader(http.StatusOK)
		writeHealthJSON(w, "ok", h.version, "ok")
		return
	}
	w.WriteHeader(http.StatusOK)
	writeHealthJSON(w, "ok", h.version, "")
}

func writeHealthJSON(w http.ResponseWriter, status, version, dbStatus string) {
	m := map[string]string{"status": status}
	if version != "" {
		m["version"] = version
	}
	if dbStatus != "" {
		m["db"] = dbStatus
	}
	_ = json.NewEncoder(w).Encode(m)
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
	Token        string `json:"token,omitempty"`
	TotpRequired bool   `json:"totp_required,omitempty"`
	TotpToken    string `json:"totp_token,omitempty"`
}

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		if i := strings.Index(x, ","); i > 0 {
			return strings.TrimSpace(x[:i])
		}
		return strings.TrimSpace(x)
	}
	return r.RemoteAddr
}

func getToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token != "" && len(token) > 7 && token[:7] == "Bearer " {
		return token[7:]
	}
	return r.URL.Query().Get("token")
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if h.authLimiter != nil && !h.authLimiter.Allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
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
		log.Printf("api register: failed username=%q: %v", req.Username, err)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken or error"})
		return
	}
	log.Printf("api register: ok username=%q", req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if h.authLimiter != nil && !h.authLimiter.Allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
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
	if h.sessionSecret != "" {
		userID, err := h.auth.Authenticate(r.Context(), req.Username, req.Password)
		if err == nil {
			enabled, _ := h.store.IsTOTPEnabled(r.Context(), userID)
			if enabled {
				totpToken := signTOTPToken(h.sessionSecret, userID)
				writeJSON(w, http.StatusOK, loginResponse{TotpRequired: true, TotpToken: totpToken})
				return
			}
		}
	}
	_, token, err := h.auth.Login(r.Context(), req.Username, req.Password, req.ClientName, req.ClientOS)
	if err != nil {
		log.Printf("api login: failed username=%q: %v", req.Username, err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"})
		return
	}
	log.Printf("api login: ok username=%q client=%q os=%q", req.Username, req.ClientName, req.ClientOS)
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

const totpTokenExpiry = 5 * 60 // seconds

func signTOTPToken(secret, userID string) string {
	expiry := time.Now().Unix() + totpTokenExpiry
	payload := userID + "|" + strconv.FormatInt(expiry, 10)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	sig := hex.EncodeToString(h.Sum(nil))
	return base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func verifyTOTPToken(secret, token string) (userID string, ok bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	payload := string(payloadBytes)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	expected := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", false
	}
	idx := strings.LastIndex(payload, "|")
	if idx < 0 {
		return "", false
	}
	expiry, err := strconv.ParseInt(payload[idx+1:], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", false
	}
	return payload[:idx], true
}

type loginTOTPRequest struct {
	TotpToken  string `json:"totp_token"`
	Code       string `json:"code"`
	ClientName string `json:"client_name"`
	ClientOS   string `json:"client_os"`
}

func (h *Handler) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if h.sessionSecret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "totp step not supported"})
		return
	}
	if h.authLimiter != nil && !h.authLimiter.Allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
	var req loginTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	userID, ok := verifyTOTPToken(h.sessionSecret, req.TotpToken)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired totp_token"})
		return
	}
	secret, err := h.store.GetTOTPSecret(r.Context(), userID)
	if err != nil || secret == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "2FA not enabled or invalid"})
		return
	}
	if !totp.Validate(strings.TrimSpace(req.Code), secret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid code"})
		return
	}
	if req.ClientOS == "" {
		req.ClientOS = "unknown"
	}
	if req.ClientName == "" {
		req.ClientName = "client"
	}
	token, err := h.store.RegisterClient(r.Context(), userID, req.ClientName, req.ClientOS)
	if err != nil {
		log.Printf("api login/totp: register client failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
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
		token = strings.TrimSpace(token)
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

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.NewPassword == "" || len(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}
	hash, err := h.store.UserPasswordHash(r.Context(), userID)
	if err != nil || hash == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token or user"})
		return
	}
	if err := auth.CheckPassword(req.CurrentPassword, hash); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is wrong"})
		return
	}
	if err := h.auth.ChangePassword(r.Context(), userID, req.NewPassword); err != nil {
		log.Printf("api change-password: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleSaveSummaries(w http.ResponseWriter, r *http.Request, userID string) {
	limit, offset := parseLimitOffset(r)
	var summaries []store.SaveSummary
	var total int
	var err error
	if limit > 0 || offset > 0 {
		summaries, total, err = h.store.ListSaveSummariesPaginated(r.Context(), userID, limit, offset)
	} else {
		summaries, err = h.store.ListSaveSummaries(r.Context(), userID)
		if err == nil {
			total = len(summaries)
		}
	}
	if err != nil {
		log.Printf("api save summaries: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	resp := map[string]interface{}{"saves": summaries}
	if limit > 0 || offset > 0 {
		resp["total"] = total
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleListClients(w http.ResponseWriter, r *http.Request, userID string) {
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("api list clients: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	items := make([]map[string]string, len(clients))
	for i := range clients {
		items[i] = map[string]string{"id": clients[i].ID, "name": clients[i].Name, "os": clients[i].OS, "last_seen": clients[i].LastSeen}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"clients": items})
}

func (h *Handler) handleListSaveVersions(w http.ResponseWriter, r *http.Request, userID string) {
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id and path_key required"})
		return
	}
	versions, err := h.store.ListSaveVersions(r.Context(), userID, gameID, pathKey, 20)
	if err != nil {
		log.Printf("api list save versions: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func (h *Handler) handleGetSaveVersion(w http.ResponseWriter, r *http.Request, userID string) {
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	versionStr := strings.TrimSpace(r.URL.Query().Get("version"))
	if gameID == "" || pathKey == "" || versionStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id, path_key and version required"})
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	blob, err := h.store.GetSaveVersion(r.Context(), userID, gameID, pathKey, version)
	if err != nil {
		log.Printf("api get save version: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
		return
	}
	if blob == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"game_id": blob.GameID, "path_key": blob.PathKey, "version": version,
		"updated_at": blob.UpdatedAt, "content": encodeBase64(blob.Content),
	})
}

func (h *Handler) handleRestoreSaveVersion(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		GameID  string `json:"game_id"`
		PathKey string `json:"path_key"`
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.GameID = strings.TrimSpace(req.GameID)
	req.PathKey = strings.TrimSpace(req.PathKey)
	if req.GameID == "" || req.PathKey == "" || req.Version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id, path_key and version required"})
		return
	}
	if err := h.store.RestoreSaveVersion(r.Context(), userID, req.GameID, req.PathKey, req.Version); err != nil {
		log.Printf("api restore save version: %v", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found or restore failed"})
		return
	}
	if username, _ := h.store.UsernameByID(r.Context(), userID); username != "" {
		_ = h.store.AppendAudit(r.Context(), userID, username, "restore_version", "", fmt.Sprintf("game_id=%s path_key=%s version=%d", req.GameID, req.PathKey, req.Version))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pullSaveItem is the JSON shape for one save (content as base64).
type pullSaveItem struct {
	GameID    string `json:"game_id"`
	PathKey   string `json:"path_key"`
	UpdatedAt string `json:"updated_at"`
	Content   string `json:"content"` // base64
}

func (h *Handler) handlePull(w http.ResponseWriter, r *http.Request, userID string) {
	limit, offset := parseLimitOffset(r)
	var saves []types.SaveBlob
	var total int
	var err error
	if limit > 0 || offset > 0 {
		saves, total, err = h.store.ListSavesPaginated(r.Context(), userID, limit, offset)
	} else {
		saves, err = h.store.ListSaves(r.Context(), userID)
		if err == nil {
			total = len(saves)
		}
	}
	if err != nil {
		log.Printf("api pull: list failed user=%s: %v", userID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	log.Printf("api pull: user=%s saves=%d", userID, len(saves))
	items := make([]pullSaveItem, len(saves))
	for i := range saves {
		items[i] = pullSaveItem{
			GameID:    saves[i].GameID,
			PathKey:   saves[i].PathKey,
			UpdatedAt: saves[i].UpdatedAt,
			Content:   encodeBase64(saves[i].Content),
		}
	}
	resp := map[string]interface{}{"saves": items}
	if limit > 0 || offset > 0 {
		resp["total"] = total
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handlePush(w http.ResponseWriter, r *http.Request, userID string) {
	if h.readOnly {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is in read-only mode"})
		return
	}
	if h.pushLimiter != nil && !h.pushLimiter.Allow(userID) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
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
	if len(filePath) > 2048 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-File-Path too long"})
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
	// Global storage quota check (0 = unlimited)
	if h.maxStorageBytes > 0 {
		total, _ := h.store.TotalStorageBytes(r.Context())
		if total+int64(len(content)) > h.maxStorageBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "global storage limit exceeded"})
			return
		}
	}
	// Per-user storage quota check (0 = unlimited)
	if quota, err := h.store.UserQuotaBytes(r.Context(), userID); err == nil && quota > 0 {
		current, _ := h.store.UserStorageBytes(r.Context(), userID)
		if current+int64(len(content)) > quota {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "storage quota exceeded"})
			return
		}
	}
	log.Printf("push: user=%s game=%s path_key=%s file=%s size=%d", userID, gameID, pathKey, filePath, len(content))
	if err := h.store.UpsertSave(r.Context(), userID, gameID, pathKey, content); err != nil {
		log.Printf("api push: upsert failed user=%s game=%s path_key=%s: %v", userID, gameID, pathKey, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleDeleteSave(w http.ResponseWriter, r *http.Request, userID string) {
	if h.readOnly {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is in read-only mode"})
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id and path_key required"})
		return
	}
	if len(gameID) > 512 || len(pathKey) > 1024 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id or path_key too long"})
		return
	}
	if err := h.store.DeleteSave(r.Context(), userID, gameID, pathKey); err != nil {
		log.Printf("api delete save: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
		return
	}
	if username, _ := h.store.UsernameByID(r.Context(), userID); username != "" {
		_ = h.store.AppendAudit(r.Context(), userID, username, "delete_save", "", fmt.Sprintf("game_id=%s path_key=%s", gameID, pathKey))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleManifest(w http.ResponseWriter, r *http.Request) {
	if h.manifestLimiter != nil {
		key := clientIP(r)
		if userID, _, err := h.auth.ValidateToken(r.Context(), getToken(r)); err == nil {
			key = userID
		}
		if !h.manifestLimiter.Allow(key) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
	}
	since := r.URL.Query().Get("since")
	var entries []types.GameSaveLocation
	var err error
	if since != "" {
		entries, err = h.store.GetManifestSince(r.Context(), since)
	} else {
		// Use write lock for the entire check-and-populate to avoid TOCTOU race
		// where two goroutines both see a stale cache and both fetch from DB.
		h.manifestCache.mu.Lock()
		if time.Since(h.manifestCache.at) < manifestCacheTTL && len(h.manifestCache.entries) > 0 {
			entries = h.manifestCache.entries
			h.manifestCache.mu.Unlock()
		} else {
			h.manifestCache.mu.Unlock()
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
		log.Printf("api manifest: list failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "manifest failed"})
		return
	}
	include := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include")))
	if include == "" {
		include = "both"
	}
	if include == "saves" || include == "config" {
		filtered := entries[:0]
		for _, e := range entries {
			if include == "saves" && !e.IsConfig {
				filtered = append(filtered, e)
			} else if include == "config" && e.IsConfig {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if since != "" {
		log.Printf("api manifest: delta since=%s entries=%d", since, len(entries))
	} else {
		log.Printf("api manifest: full entries=%d (include=%s)", len(entries), include)
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

	total := len(entries)
	manifestLimit, manifestOffset := parseLimitOffset(r)
	if manifestLimit > 0 || manifestOffset > 0 {
		if manifestOffset > total {
			entries = nil
		} else {
			end := manifestOffset + manifestLimit
			if manifestLimit <= 0 {
				end = total
			}
			if end > total {
				end = total
			}
			entries = entries[manifestOffset:end]
		}
	}
	resp := map[string]interface{}{"entries": entries}
	if manifestLimit > 0 || manifestOffset > 0 {
		resp["total"] = total
	}
	writeJSON(w, http.StatusOK, resp)
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

// parseLimitOffset parses limit and offset from query params. Returns 0,0 if not set or invalid.
func parseLimitOffset(r *http.Request) (limit, offset int) {
	if s := r.URL.Query().Get("limit"); s != "" {
		fmt.Sscanf(s, "%d", &limit)
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		fmt.Sscanf(s, "%d", &offset)
	}
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
