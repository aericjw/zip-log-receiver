// Package checkpoint persists per-zip-entry read progress so a restart resumes instead of re-ingesting.
package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Persister is the subset of the stanza operator.Persister the store needs.
// A Get on a missing key returns (nil, nil).
type Persister interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
}

// EntryRef identifies one entry's content inside one zip. The full zip path is part of the
// identity on purpose: a copy at a new path is re-ingested (duplicates) rather than skipped (loss).
type EntryRef struct {
	ZipPath string
	Entry   string
	CRC32   uint32
	Size    uint64
}

// EntryState is the saved progress of an entry.
type EntryState struct {
	// Records is how many records have been emitted downstream.
	Records int64 `json:"records"`
	// Done marks the entry fully ingested.
	Done bool `json:"done"`
}

// Store reads and writes EntryState through a Persister.
type Store struct{ p Persister }

// NewStore wraps p.
func NewStore(p Persister) *Store { return &Store{p: p} }

func (s *Store) key(r EntryRef) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", r.ZipPath, r.Entry, r.CRC32, r.Size)))
	return "zip." + hex.EncodeToString(sum[:])
}

// Get returns the saved state, or the zero state if the entry was never seen.
func (s *Store) Get(ctx context.Context, r EntryRef) (EntryState, error) {
	b, err := s.p.Get(ctx, s.key(r))
	if err != nil {
		return EntryState{}, fmt.Errorf("read checkpoint: %w", err)
	}
	if b == nil {
		return EntryState{}, nil
	}
	var st EntryState
	if err := json.Unmarshal(b, &st); err != nil {
		return EntryState{}, fmt.Errorf("decode checkpoint for %s!%s: %w", r.ZipPath, r.Entry, err)
	}
	return st, nil
}

// Set saves the state for r.
func (s *Store) Set(ctx context.Context, r EntryRef, st EntryState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	if err := s.p.Set(ctx, s.key(r), b); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}
