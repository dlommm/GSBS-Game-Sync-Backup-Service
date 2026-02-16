package auth

import (
	"context"
	"errors"

	"github.com/gsbs/gsbs/server/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrBadCredentials = errors.New("bad credentials")
)

// Service handles user auth and client tokens.
type Service struct {
	store store.Store
}

func NewService(st store.Store) *Service {
	return &Service{store: st}
}

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword returns nil if password matches hash.
func CheckPassword(password, hash string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return ErrBadCredentials
	}
	return nil
}

// RegisterUser creates a user and returns user ID.
func (s *Service) RegisterUser(ctx context.Context, username, password string) (userID string, err error) {
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	return s.store.CreateUser(ctx, username, hash)
}

// isUserDisabled returns true if the user account is disabled.
func (s *Service) isUserDisabled(ctx context.Context, userID string) bool {
	ok, err := s.store.IsUserDisabled(ctx, userID)
	return err == nil && ok
}

// Login validates username/password and returns a new client token (for a new "client" device).
func (s *Service) Login(ctx context.Context, username, password, clientName, clientOS string) (userID, clientToken string, err error) {
	uid, hash, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return "", "", ErrBadCredentials
	}
	if s.isUserDisabled(ctx, uid) {
		return "", "", ErrBadCredentials
	}
	if err := CheckPassword(password, hash); err != nil {
		return "", "", err
	}
	token, err := s.store.RegisterClient(ctx, uid, clientName, clientOS)
	if err != nil {
		return "", "", err
	}
	return uid, token, nil
}

// Authenticate validates username/password and returns the userID without registering a client device.
// Use this for web UI logins that only need a session, not a device token.
func (s *Service) Authenticate(ctx context.Context, username, password string) (userID string, err error) {
	uid, hash, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return "", ErrBadCredentials
	}
	if s.isUserDisabled(ctx, uid) {
		return "", ErrBadCredentials
	}
	if err := CheckPassword(password, hash); err != nil {
		return "", err
	}
	return uid, nil
}

// ChangePassword updates the user's password. Caller must have verified current password (e.g. via Authenticate).
func (s *Service) ChangePassword(ctx context.Context, userID, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.store.UpdateUserPassword(ctx, userID, hash)
}

// ValidateToken returns userID and clientID if the token is valid.
// Returns error if the user is disabled.
func (s *Service) ValidateToken(ctx context.Context, token string) (userID, clientID string, err error) {
	userID, clientID, _, _, err = s.store.ClientByToken(ctx, token)
	if err != nil {
		return "", "", err
	}
	if s.isUserDisabled(ctx, userID) {
		return "", "", ErrBadCredentials
	}
	return userID, clientID, nil
}
