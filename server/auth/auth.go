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

// Login validates username/password and returns a new client token (for a new "client" device).
func (s *Service) Login(ctx context.Context, username, password, clientName, clientOS string) (userID, clientToken string, err error) {
	uid, hash, err := s.store.UserByUsername(ctx, username)
	if err != nil {
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

// ValidateToken returns userID and clientID if the token is valid.
func (s *Service) ValidateToken(ctx context.Context, token string) (userID, clientID string, err error) {
	userID, clientID, _, _, err = s.store.ClientByToken(ctx, token)
	return userID, clientID, err
}
