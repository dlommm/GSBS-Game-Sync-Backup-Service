package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// IntegrityResult summarizes one integrity_check run.
type IntegrityResult struct {
	Checked          int // unencrypted slots verified
	SkippedEncrypted int // slots the server cannot verify (client-side encryption)
	Mismatched       int // stored hash != recomputed hash
	MissingFile      int // filesystem-mode blob file gone
	Unreadable       int // blob file present but unreadable
}

// Problems is the number of slots with a recorded finding.
func (r IntegrityResult) Problems() int { return r.Mismatched + r.MissingFile + r.Unreadable }

// IntegrityFinding is one save slot that failed verification.
type IntegrityFinding struct {
	At           string
	UserID       string
	Username     string
	GameID       string
	PathKey      string
	Kind         string // hash_mismatch | missing_file | unreadable
	ExpectedHash string
	ActualHash   string
}

// Finding kinds recorded by RunIntegrityCheck.
const (
	IntegrityKindHashMismatch = "hash_mismatch"
	IntegrityKindMissingFile  = "missing_file"
	IntegrityKindUnreadable   = "unreadable"
)

type integritySlotFinding struct {
	userID, gameID, pathKey string
	kind                    string
	expected, actual        string
}

// RunIntegrityCheck re-hashes every unencrypted save (blob or filesystem
// file) against its stored content_hash. Problems are recorded in
// integrity_findings (one row per slot, replaced on re-check); slots that
// verify clean — or that can no longer be verified (now encrypted) — have any
// stale finding removed.
func (s *sqliteStore) RunIntegrityCheck(ctx context.Context) (IntegrityResult, error) {
	var res IntegrityResult
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, game_id, path_key, COALESCE(content_hash, ''), encrypted, storage_path, content FROM saves`)
	if err != nil {
		return res, err
	}
	var findings []integritySlotFinding
	var clean [][3]string
	for rows.Next() {
		var userID, gameID, pathKey, storedHash string
		var encrypted int
		var storagePath sql.NullString
		var content []byte
		if err := rows.Scan(&userID, &gameID, &pathKey, &storedHash, &encrypted, &storagePath, &content); err != nil {
			rows.Close()
			return res, err
		}
		if ctx.Err() != nil {
			rows.Close()
			return res, ctx.Err()
		}
		slot := [3]string{userID, gameID, pathKey}
		if encrypted != 0 {
			res.SkippedEncrypted++
			clean = append(clean, slot) // cannot verify; drop stale findings
			continue
		}
		data := content
		if storagePath.Valid && storagePath.String != "" {
			b, rerr := os.ReadFile(storagePath.String)
			switch {
			case os.IsNotExist(rerr):
				findings = append(findings, integritySlotFinding{userID, gameID, pathKey, IntegrityKindMissingFile, storedHash, ""})
				res.MissingFile++
				continue
			case rerr != nil:
				findings = append(findings, integritySlotFinding{userID, gameID, pathKey, IntegrityKindUnreadable, storedHash, ""})
				res.Unreadable++
				continue
			}
			data = b
		}
		res.Checked++
		actual := hashContent(data)
		if storedHash != "" && actual != storedHash {
			findings = append(findings, integritySlotFinding{userID, gameID, pathKey, IntegrityKindHashMismatch, storedHash, actual})
			res.Mismatched++
			continue
		}
		clean = append(clean, slot)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, err
	}
	rows.Close()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range findings {
		id, idErr := genID()
		if idErr != nil {
			_ = tx.Rollback()
			return res, idErr
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO integrity_findings (id, at, user_id, game_id, path_key, kind, expected_hash, actual_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, game_id, path_key) DO UPDATE SET
				at = excluded.at, kind = excluded.kind,
				expected_hash = excluded.expected_hash, actual_hash = excluded.actual_hash`,
			id, now, f.userID, f.gameID, f.pathKey, f.kind, f.expected, f.actual); err != nil {
			_ = tx.Rollback()
			return res, fmt.Errorf("record finding: %w", err)
		}
	}
	for _, slot := range clean {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM integrity_findings WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			slot[0], slot[1], slot[2]); err != nil {
			_ = tx.Rollback()
			return res, fmt.Errorf("clear finding: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// CountIntegrityFindings returns the number of slots with an open finding.
func (s *sqliteStore) CountIntegrityFindings(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM integrity_findings`).Scan(&n)
	return n, err
}

// ListIntegrityFindings returns open findings, newest first.
func (s *sqliteStore) ListIntegrityFindings(ctx context.Context, limit int) ([]IntegrityFinding, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.at, f.user_id, COALESCE(u.username, ''), f.game_id, f.path_key, f.kind,
		       COALESCE(f.expected_hash, ''), COALESCE(f.actual_hash, '')
		FROM integrity_findings f
		LEFT JOIN users u ON u.id = f.user_id
		ORDER BY f.at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntegrityFinding
	for rows.Next() {
		var f IntegrityFinding
		if err := rows.Scan(&f.At, &f.UserID, &f.Username, &f.GameID, &f.PathKey, &f.Kind, &f.ExpectedHash, &f.ActualHash); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
