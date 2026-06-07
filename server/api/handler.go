package api

import (
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/savepath"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/netutil"
	"github.com/gsbs/gsbs/server/ratelimit"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/pquerna/otp/totp"
)

// MaxSaveSize is the maximum allowed body size for POST /api/saves (50 MiB).
const MaxSaveSize = 50 * 1024 * 1024

// contextKey is a typed key for values stored in request contexts.
type contextKey string

const contextClientID contextKey = "client_id"

const maxGameIDLen = 512
const maxPathKeyLen = 1024

func saveKeyTooLong(gameID, pathKey string) string {
	if len(gameID) > maxGameIDLen || len(pathKey) > maxPathKeyLen {
		return "game_id or path_key too long"
	}
	return ""
}

// maxAuthBody is the maximum JSON body size for auth endpoints (1 MiB).
const maxAuthBody = 1 << 20

// Handler is the HTTP API handler.
type Handler struct {
	store           store.Store
	auth            *auth.Service
	allowRegister   bool
	hub             *sse.Hub
	authLimiter     *ratelimit.Limiter
	pushLimiter     *ratelimit.Limiter
	pullLimiter     *ratelimit.Limiter
	generalLimiter  *ratelimit.Limiter
	manifestLimiter *ratelimit.Limiter
	maxStorageBytes int64  // 0 = unlimited
	readOnly        bool   // if true, reject push and delete
	sessionSecret   string // for signing TOTP step token when 2FA enabled; empty = no API 2FA
	version         string // server version for health endpoint

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
func NewHandler(st store.Store, authSvc *auth.Service, allowRegister bool, hub *sse.Hub, authLimiter, pushLimiter, pullLimiter, generalLimiter, manifestLimiter *ratelimit.Limiter, maxStorageBytes int64, readOnly bool, sessionSecret string, version string) *Handler {
	return &Handler{store: st, auth: authSvc, allowRegister: allowRegister, hub: hub, authLimiter: authLimiter, pushLimiter: pushLimiter, pullLimiter: pullLimiter, generalLimiter: generalLimiter, manifestLimiter: manifestLimiter, maxStorageBytes: maxStorageBytes, readOnly: readOnly, sessionSecret: sessionSecret, version: version}
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
	case r.URL.Path == "/api/manifest/v2" && r.Method == http.MethodGet:
		h.handleManifestV2(w, r)
	case r.URL.Path == "/api/clients" && r.Method == http.MethodGet:
		h.withAuth(h.handleListClients)(w, r)
	case r.URL.Path == "/api/clients/revoke" && r.Method == http.MethodPost:
		h.withAuth(h.handleRevokeClient)(w, r)
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
	case r.URL.Path == "/api/token/refresh" && r.Method == http.MethodPost:
		h.withAuth(h.handleTokenRefresh)(w, r)
	case r.URL.Path == "/api/account" && r.Method == http.MethodGet:
		h.withAuth(h.handleAccount)(w, r)
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

func getToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token != "" && len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		return strings.TrimSpace(token[7:])
	}
	if q := r.URL.Query().Get("token"); q != "" {
		logx.Logger().Warn().
			Str("path", r.URL.Path).
			Str("ip", netutil.ClientIP(r)).
			Msg("token query param auth rejected; use Authorization: Bearer")
	}
	return ""
}

func (h *Handler) rateLimited(w http.ResponseWriter, r *http.Request, limiter *ratelimit.Limiter, key, label string) bool {
	if limiter != nil && !limiter.Allow(key) {
		logx.Logger().Warn().
			Str("path", r.URL.Path).
			Str("ip", netutil.ClientIP(r)).
			Str("key", key).
			Str("limit", label).
			Msg("rate limit exceeded")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return true
	}
	return false
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if h.rateLimited(w, r, h.authLimiter, netutil.ClientIP(r), "auth") {
		return
	}
	if !h.allowRegister {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "registration is disabled"})
		return
	}
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody)).Decode(&req); err != nil {
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
	if len(req.Password) > 72 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too long"})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), req.Username, req.Password)
	if err != nil {
		logx.Logger().Warn().Str("username", req.Username).Err(err).Msg("api register failed")
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken or error"})
		return
	}
	logx.Logger().Info().Str("username", req.Username).Msg("api register ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if h.rateLimited(w, r, h.authLimiter, netutil.ClientIP(r), "auth") {
		return
	}
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody)).Decode(&req); err != nil {
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
	if len(req.Password) < 8 || len(req.Password) > 72 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid password length"})
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
		logx.Logger().Warn().Str("username", req.Username).Err(err).Msg("api login failed")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"})
		return
	}
	logx.Logger().Info().Str("username", req.Username).Str("client", req.ClientName).Str("os", req.ClientOS).Msg("api login ok")
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
	if h.rateLimited(w, r, h.authLimiter, netutil.ClientIP(r), "auth") {
		return
	}
	var req loginTOTPRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody)).Decode(&req); err != nil {
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
		logx.Logger().Error().Err(err).Msg("api login/totp register client failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

// withAuth wraps a handler requiring auth. Passes userID to the handler.
// Also updates client last_seen on every authenticated request and stashes
// clientID in the request context (retrievable via contextClientID) so handlers
// can use it without a second token validation round-trip.
func (h *Handler) withAuth(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := getToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
			return
		}
		userID, clientID, err := h.auth.ValidateToken(r.Context(), token)
		if err != nil {
			logx.Logger().Warn().Str("path", r.URL.Path).Err(err).Msg("api auth failed")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		// Update last_seen inline (cheap single-row UPDATE; request context keeps it bounded).
		if err := h.store.UpdateClientLastSeen(r.Context(), clientID); err != nil {
			logx.Logger().Debug().Str("client_id", clientID).Err(err).Msg("update last_seen failed")
		}
		// Stash clientID so downstream handlers avoid a redundant ValidateToken call.
		r = r.WithContext(context.WithValue(r.Context(), contextClientID, clientID))
		fn(w, r, userID)
	}
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
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
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api change-password failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleSaveSummaries(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.pullLimiter, userID, "pull") {
		return
	}
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
		errClass := classifyDBError(err)
		logx.Logger().Error().
			Str("user_id", userID).
			Int("limit", limit).
			Int("offset", offset).
			Str("request_id", strings.TrimSpace(r.Header.Get("X-Request-ID"))).
			Str("error_class", errClass).
			Err(err).
			Msg("api save summaries failed")
		code := http.StatusInternalServerError
		if errClass == "db_locked" {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]string{"error": "list failed"})
		return
	}
	items := make([]map[string]interface{}, len(summaries))
	for i, s := range summaries {
		items[i] = map[string]interface{}{
			"game_id": s.GameID, "path_key": s.PathKey, "game_title": s.GameTitle,
			"size_bytes": s.SizeBytes, "updated_at": s.UpdatedAt, "content_hash": s.ContentHash,
			"encrypted": s.Encrypted,
		}
	}
	resp := map[string]interface{}{"saves": items}
	if limit > 0 || offset > 0 {
		resp["total"] = total
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAccount(w http.ResponseWriter, r *http.Request, userID string) {
	enc, err := h.store.IsEncryptionEnabled(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account lookup failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"encryption_enabled": enc})
}

func (h *Handler) handleListClients(w http.ResponseWriter, r *http.Request, userID string) {
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api list clients failed")
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
	if msg := saveKeyTooLong(gameID, pathKey); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	versions, err := h.store.ListSaveVersions(r.Context(), userID, gameID, pathKey, 20)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api list save versions failed")
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
	if msg := saveKeyTooLong(gameID, pathKey); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	blob, err := h.store.GetSaveVersion(r.Context(), userID, gameID, pathKey, version)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api get save version failed")
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
	if h.readOnly {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is in read-only mode"})
		return
	}
	var req struct {
		GameID  string `json:"game_id"`
		PathKey string `json:"path_key"`
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.GameID = strings.TrimSpace(req.GameID)
	req.PathKey = strings.TrimSpace(req.PathKey)
	if req.GameID == "" || req.PathKey == "" || req.Version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id, path_key and version required"})
		return
	}
	if msg := saveKeyTooLong(req.GameID, req.PathKey); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if err := h.store.RestoreSaveVersion(r.Context(), userID, req.GameID, req.PathKey, req.Version); err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api restore save version failed")
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
	Encrypted bool   `json:"encrypted,omitempty"`
}

func (h *Handler) handlePull(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.pullLimiter, userID, "pull") {
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID != "" && pathKey != "" {
		if msg := saveKeyTooLong(gameID, pathKey); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		blob, err := h.store.GetSave(r.Context(), userID, gameID, pathKey)
		if err != nil {
			logx.Logger().Error().Err(err).Msg("api pull single failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get failed"})
			return
		}
		if blob == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		items := []pullSaveItem{{
			GameID:    blob.GameID,
			PathKey:   blob.PathKey,
			UpdatedAt: blob.UpdatedAt,
			Content:   encodeBase64(blob.Content),
			Encrypted: blob.Encrypted,
		}}
		resp := map[string]interface{}{"saves": items}
		writeJSON(w, http.StatusOK, resp)
		return
	}
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
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api pull list failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	logx.Logger().Debug().Str("user_id", userID).Int("saves", len(saves)).Msg("api pull")
	items := make([]pullSaveItem, len(saves))
	for i := range saves {
		items[i] = pullSaveItem{
			GameID:    saves[i].GameID,
			PathKey:   saves[i].PathKey,
			UpdatedAt: saves[i].UpdatedAt,
			Content:   encodeBase64(saves[i].Content),
			Encrypted: saves[i].Encrypted,
		}
	}
	resp := map[string]interface{}{"saves": items}
	if limit > 0 || offset > 0 {
		resp["total"] = total
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		gz := gzip.NewWriter(w)
		if err := json.NewEncoder(gz).Encode(resp); err != nil {
			logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api pull gzip encode failed")
		}
		_ = gz.Close()
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handlePush(w http.ResponseWriter, r *http.Request, userID string) {
	if h.readOnly {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is in read-only mode"})
		return
	}
	if h.rateLimited(w, r, h.pushLimiter, userID, "push") {
		return
	}
	gameID := strings.TrimSpace(r.Header.Get("X-Game-ID"))
	pathKey := strings.TrimSpace(r.Header.Get("X-Path-Key"))
	filePath := r.Header.Get("X-File-Path")
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Game-ID and X-Path-Key required"})
		return
	}
	if msg := saveKeyTooLong(gameID, pathKey); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Game-ID or X-Path-Key too long"})
		return
	}
	if len(filePath) > 2048 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-File-Path too long"})
		return
	}
	relPath := strings.TrimSpace(r.Header.Get("X-Relative-Path"))
	if relPath != "" {
		if err := savepath.ValidateRelativePath(relPath); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid X-Relative-Path"})
			return
		}
	} else if strings.TrimSpace(os.Getenv("GSBS_SAVE_ROOT")) != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-Relative-Path required"})
		return
	}
	var content []byte
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid gzip body"})
			return
		}
		defer gr.Close()
		limited := io.LimitReader(gr, MaxSaveSize+1)
		content, err = io.ReadAll(limited)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
			return
		}
	} else {
		limited := http.MaxBytesReader(nil, r.Body, MaxSaveSize)
		var err error
		content, err = io.ReadAll(limited)
		if err != nil {
			if strings.Contains(err.Error(), "request body too large") {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "save too large (max 50 MiB)"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
			return
		}
	}
	if len(content) > MaxSaveSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "save too large (max 50 MiB)"})
		return
	}
	if len(content) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty content"})
		return
	}
	existingSize, _ := h.store.GetSaveContentSize(r.Context(), userID, gameID, pathKey)
	delta := int64(len(content)) - existingSize
	// Global storage quota check (0 = unlimited)
	if h.maxStorageBytes > 0 {
		total, err := h.store.TotalStorageBytes(r.Context())
		if err != nil {
			logx.Logger().Error().
				Str("user_id", userID).
				Str("operation", "total_storage_check").
				Err(err).
				Msg("api push storage check failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage check failed"})
			return
		}
		if total+delta > h.maxStorageBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "global storage limit exceeded"})
			return
		}
	}
	// Per-user storage quota check (0 = unlimited)
	if quota, qErr := h.store.UserQuotaBytes(r.Context(), userID); qErr == nil && quota > 0 {
		current, err := h.store.UserStorageBytes(r.Context(), userID)
		if err != nil {
			logx.Logger().Error().
				Str("user_id", userID).
				Str("operation", "user_storage_check").
				Err(err).
				Msg("api push storage check failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage check failed"})
			return
		}
		if current+delta > quota {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "storage quota exceeded"})
			return
		}
	}
	// Optimistic concurrency: if X-GSBS-If-Hash is provided, reject if server hash differs.
	if ifHash := strings.TrimSpace(r.Header.Get("X-GSBS-If-Hash")); ifHash != "" {
		serverHash, serverVer, err := h.store.GetSaveHashAndVersion(r.Context(), userID, gameID, pathKey)
		if err != nil {
			logx.Logger().Error().
				Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).
				Err(err).Msg("api push hash check failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash check failed"})
			return
		}
		if serverHash != ifHash {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":           "conflict",
				"current_hash":    serverHash,
				"current_version": serverVer,
			})
			return
		}
	}
	logx.Logger().Debug().
		Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).
		Str("file", filePath).Int("size", len(content)).Msg("push")
	contentHash := strings.TrimSpace(r.Header.Get("X-Content-Hash"))
	contentSize, _ := strconv.ParseInt(r.Header.Get("X-Content-Size"), 10, 64)
	encrypted := r.Header.Get("X-Encrypted") == "1"
	// clientID was stashed in context by withAuth; no need to re-validate the token.
	clientID, _ := r.Context().Value(contextClientID).(string)
	meta := &store.SaveMeta{
		ContentHash:  contentHash,
		ContentSize:  contentSize,
		ClientID:     clientID,
		Encrypted:    encrypted,
		RelativePath: relPath,
	}
	skipped, err := h.store.UpsertSaveWithMeta(r.Context(), userID, gameID, pathKey, content, meta)
	if err != nil {
		logx.Logger().Error().
			Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).
			Err(err).Msg("api push upsert failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save failed"})
		return
	}
	if skipped {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unchanged"})
		return
	}
	if h.hub != nil {
		h.hub.BroadcastToUser(userID, sse.Event{
			Type: "save-updated",
			Data: fmt.Sprintf(`{"game_id":%q,"path_key":%q}`, gameID, pathKey),
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleDeleteSave(w http.ResponseWriter, r *http.Request, userID string) {
	if h.readOnly {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is in read-only mode"})
		return
	}
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id and path_key required"})
		return
	}
	if msg := saveKeyTooLong(gameID, pathKey); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if err := h.store.DeleteSave(r.Context(), userID, gameID, pathKey); err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("api delete save failed")
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
		key := netutil.ClientIP(r)
		if userID, _, err := h.auth.ValidateToken(r.Context(), getToken(r)); err == nil {
			key = userID
		}
		if h.rateLimited(w, r, h.manifestLimiter, key, "manifest") {
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
		logx.Logger().Error().Err(err).Msg("api manifest list failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "manifest failed"})
		return
	}
	include := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include")))
	if include == "" {
		include = "both"
	}
	if include == "saves" || include == "config" {
		filtered := make([]types.GameSaveLocation, 0, len(entries))
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
		logx.Logger().Debug().Str("since", since).Int("entries", len(entries)).Msg("api manifest delta")
	} else {
		logx.Logger().Debug().Int("entries", len(entries)).Str("include", include).Msg("api manifest full")
	}

	// Log the fetch (best-effort). If auth token is present, resolve client info.
	go func() {
		ctx := context.Background()
		clientID, clientName, username := "", "", ""
		token := getToken(r)
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
	h.writeManifestHeaders(w, r)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeManifestHeaders(w http.ResponseWriter, r *http.Request) {
	meta, err := h.store.GetPCGWManifestMeta(r.Context())
	if err != nil || meta == nil {
		return
	}
	if meta.ManifestETag != "" {
		w.Header().Set("ETag", meta.ManifestETag)
	}
	if meta.ManifestVersion > 0 {
		w.Header().Set("X-Manifest-Version", strconv.Itoa(meta.ManifestVersion))
	}
}

func (h *Handler) handleManifestV2(w http.ResponseWriter, r *http.Request) {
	if h.manifestLimiter != nil {
		key := netutil.ClientIP(r)
		if userID, _, err := h.auth.ValidateToken(r.Context(), getToken(r)); err == nil {
			key = userID
		}
		if h.rateLimited(w, r, h.manifestLimiter, key, "manifest") {
			return
		}
	}
	since := r.URL.Query().Get("since")
	platform := r.URL.Query().Get("platform")
	limit, offset := parseLimitOffset(r)
	if limit <= 0 {
		limit = 10000
	}

	meta, _ := h.store.GetPCGWManifestMeta(r.Context())
	if meta != nil && meta.ManifestETag != "" {
		w.Header().Set("ETag", meta.ManifestETag)
		if inm := r.Header.Get("If-None-Match"); inm != "" && inm == meta.ManifestETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if meta.ManifestVersion > 0 {
			w.Header().Set("X-Manifest-Version", strconv.Itoa(meta.ManifestVersion))
		}
	}

	out, err := h.store.BuildManifestV2(r.Context(), since, platform, limit, offset)
	if err != nil {
		logx.Logger().Error().Err(err).Msg("api manifest v2 failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "manifest v2 failed"})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSSE streams server-sent events to an authenticated client.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
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

	// Cap at 5 concurrent SSE connections per user; oldest is evicted if exceeded.
	ch, unsub := h.hub.SubscribeCapped(userID, 5)
	defer unsub()

	// Send initial heartbeat so the client knows the connection is live.
	fmt.Fprint(w, ": heartbeat\n\n")
	flusher.Flush()

	// Periodic heartbeat keeps the connection alive through proxies that drop idle SSE streams.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

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
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
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
	if limit > 500 {
		limit = 500
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

// classifyDBError inspects the error message and returns a short classification
// string used for structured logging and status-code selection.
// Recognised classes: "db_locked", "context_canceled", "schema_error", "unknown".
func classifyDBError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY") {
		return "db_locked"
	}
	if strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded") {
		return "context_canceled"
	}
	if strings.Contains(msg, "no such column") || strings.Contains(msg, "no such table") || strings.Contains(msg, "schema") {
		return "schema_error"
	}
	return "unknown"
}
