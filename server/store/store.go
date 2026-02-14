package store

import (
	"context"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

// Store is the persistence layer for users, clients, and saves.
type Store interface {
	// User
	CreateUser(ctx context.Context, username, passwordHash string) (userID string, err error)
	UserByUsername(ctx context.Context, username string) (userID, passwordHash string, err error)
	// UsernameByID returns the username for a user ID (e.g. for dashboard header and admin check).
	UsernameByID(ctx context.Context, userID string) (username string, err error)

	// Client
	RegisterClient(ctx context.Context, userID, name, os string) (clientID string, err error)
	ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error)
	ListClientsByUserID(ctx context.Context, userID string) ([]ClientInfo, error)
	// RegenerateClientToken issues a new token for the client; the old token is invalidated (client must re-login).
	RegenerateClientToken(ctx context.Context, clientID string) error

	// Save
	UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error
	ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error)
	GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error)

	// Game save locations (manifest from PCGW)
	UpsertGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error
	ListGameSaveLocations(ctx context.Context) ([]types.GameSaveLocation, error)
	GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error)

	// Admin / stats (used by WebUI admin page)
	CountUsers(ctx context.Context) (int, error)
	CountClients(ctx context.Context) (int, error)
	CountSaves(ctx context.Context) (int, error)
	CountGameSaveLocations(ctx context.Context) (int, error)
	ListUsers(ctx context.Context) ([]UserInfo, error)             // All users for admin listing
	ListAllClients(ctx context.Context) ([]ClientInfoWithUser, error) // All clients with owner username

	// Close releases resources (e.g. DB connection).
	Close() error
}

// SaveRecord is the internal save row.
type SaveRecord struct {
	UserID   string
	GameID   string
	PathKey  string
	Content  []byte
	UpdatedAt time.Time
}

// ClientInfo is a client row for listing (dashboard).
type ClientInfo struct {
	ID       string
	Name     string
	OS       string
	LastSeen string
}

// UserInfo is a user row for admin listing.
type UserInfo struct {
	ID        string
	Username  string
	CreatedAt string
}

// ClientInfoWithUser is a client with owning username for admin listing.
type ClientInfoWithUser struct {
	ID       string
	UserID   string
	Username string
	Name     string
	OS       string
	LastSeen string
}
