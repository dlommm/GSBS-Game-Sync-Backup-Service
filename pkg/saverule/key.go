package saverule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/gsbs/gsbs/pkg/types"
)

// RuleKey returns a stable key for (gameID, save rule).
//
// When rule.SlotLabel is non-empty (PCGW-sourced rules), the key is derived from
// (game_id, slot_label, is_config) — making it identical across Windows and Linux
// for the same logical save slot.
//
// When rule.SlotLabel is empty (user-defined rules), the key is derived from the
// full rule JSON for backward compatibility.
func RuleKey(gameID string, rule types.SaveRule) string {
	if rule.SlotLabel != "" {
		isConfig := "0"
		if rule.IsConfig {
			isConfig = "1"
		}
		h := sha256.Sum256([]byte(gameID + "\x00" + rule.SlotLabel + "\x00" + isConfig))
		return hex.EncodeToString(h[:])[:16]
	}
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
