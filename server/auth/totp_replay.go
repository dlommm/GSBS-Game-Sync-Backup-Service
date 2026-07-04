package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
)

// A TOTP code stays valid for its whole 30s step (plus skew), so a code that
// was just used to log in can be replayed by anyone who observed it. Accepted
// codes are therefore remembered per user until they expire naturally.
const totpReplayTTL = 95 * time.Second

var (
	totpReplayMu   sync.Mutex
	totpReplaySeen = map[string]time.Time{}
)

// ValidateTOTPOnce validates a TOTP code and rejects codes already accepted
// for this user within the replay window. In-memory state is correct here:
// the server is a single process, and a restart merely shrinks the window to
// the code's natural lifetime.
func ValidateTOTPOnce(userID, code, secret string) bool {
	if !totp.Validate(code, secret) {
		return false
	}
	sum := sha256.Sum256([]byte(userID + "\x00" + code))
	key := hex.EncodeToString(sum[:])
	now := time.Now()
	totpReplayMu.Lock()
	defer totpReplayMu.Unlock()
	for k, exp := range totpReplaySeen {
		if now.After(exp) {
			delete(totpReplaySeen, k)
		}
	}
	if exp, seen := totpReplaySeen[key]; seen && now.Before(exp) {
		return false
	}
	totpReplaySeen[key] = now.Add(totpReplayTTL)
	return true
}
