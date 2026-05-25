package saverule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/gsbs/gsbs/pkg/types"
)

// RuleKey returns a stable key for (gameID, save rule).
func RuleKey(gameID string, rule types.SaveRule) string {
	payload, _ := json.Marshal(struct {
		GameID string `json:"game_id"`
		types.SaveRule
	}{GameID: gameID, SaveRule: rule})
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])[:16]
}

// PathKeyForFile returns a stable path key for a file within a rule.
func PathKeyForFile(ruleKey, relativePath string) string {
	h := sha256.Sum256([]byte(ruleKey + "\x00" + relativePath))
	return hex.EncodeToString(h[:])[:16]
}
