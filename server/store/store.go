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

	// Client
	RegisterClient(ctx context.Context, userID, name, os string) (clientID string, err error)
	ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error)
	ListClientsByUserID(ctx context.Context, userID string) ([]ClientInfo, error)

	// Save
	UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error
	ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error)
	GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error)

	// Game save locations (manifest from PCGW)
	UpsertGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error
	ListGameSaveLocations(ctx context.Context) ([]types.GameSaveLocation, error)
	GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error)
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
