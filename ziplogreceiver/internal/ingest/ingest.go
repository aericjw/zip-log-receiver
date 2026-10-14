// Package ingest scans zip archives once, emitting each entry's records with resumable checkpoints.
package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/checkpoint"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/tokenize"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/zipscan"
)

// Config selects and reads zip content.
type Config struct {
	// Include and Exclude are globs (with **) over zip file paths.
	Include, Exclude []string
	// EntryInclude and EntryExclude are globs over entry names inside a zip. Empty EntryInclude means all.
	EntryInclude, EntryExclude []string
	Limits                     zipscan.Limits

	Split     bufio.SplitFunc
	Trim      func([]byte) []byte
	BatchSize int
	MaxToken  int
}

// EmitFunc receives a batch of records from one zip entry plus its attributes and the running
// record number of the last record. Returning an error stops that entry; progress is kept up to
// the last batch that was accepted.
type EmitFunc func(ctx context.Context, tokens [][]byte, attrs map[string]any, lastRecord int64) error

// Scanner performs one pass over the configured zips.
type Scanner struct {
	cfg   Config
	store *checkpoint.Store
	emit  EmitFunc
}

// New builds a Scanner.
func New(cfg Config, store *checkpoint.Store, emit EmitFunc) *Scanner {
	return &Scanner{cfg: cfg, store: store, emit: emit}
}

// Scan reads every matching zip once. Problems with one zip or entry are collected and the scan
// carries on, so a single bad archive cannot block a backfill. All collected errors are returned
// joined; a nil result means everything matched was ingested or already done.
func (s *Scanner) Scan(ctx context.Context) error {
	zips, err := s.matchZips()
	if err != nil {
		return err
	}
	var errs []error
	for _, zp := range zips {
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
		if err := s.scanZip(ctx, zp); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Scanner) matchZips() ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, pat := range s.cfg.Include {
		matches, err := doublestar.FilepathGlob(pat)
		if err != nil {
			return nil, fmt.Errorf("include pattern %q: %w", pat, err)
		}
		for _, m := range matches {
			if _, dup := seen[m]; dup || matchesAny(s.cfg.Exclude, m, true) {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Scanner) scanZip(ctx context.Context, zipPath string) error {
	entries, err := zipscan.ListEntries(zipPath)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if !s.wantEntry(e.Name) {
			continue
		}
		if err := s.scanEntry(ctx, zipPath, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Scanner) wantEntry(name string) bool {
	if len(s.cfg.EntryInclude) > 0 && !matchesAny(s.cfg.EntryInclude, name, false) {
		return false
	}
	return !matchesAny(s.cfg.EntryExclude, name, false)
}

func (s *Scanner) scanEntry(ctx context.Context, zipPath string, e zipscan.Entry) error {
	ref := checkpoint.EntryRef{ZipPath: zipPath, Entry: e.Name, CRC32: e.CRC32, Size: e.UncompressedSize}
	st, err := s.store.Get(ctx, ref)
	if err != nil {
		return err
	}
	if st.Done {
		return nil
	}

	rc, err := zipscan.OpenEntry(zipPath, e.Name, s.cfg.Limits)
	if err != nil {
		return err
	}
	defer rc.Close()

	attrs := map[string]any{
		"log.file.name": filepath.Base(zipPath),
		"log.file.path": zipPath,
		"log.zip.entry": e.Name,
	}
	total, err := tokenize.Tokenize(rc, tokenize.Options{
		Split:     s.cfg.Split,
		Trim:      s.cfg.Trim,
		BatchSize: s.cfg.BatchSize,
		Skip:      st.Records,
		MaxToken:  s.cfg.MaxToken,
	}, func(tokens [][]byte, lastRecord int64) error {
		if err := s.emit(ctx, tokens, attrs, lastRecord); err != nil {
			return err
		}
		// Commit only after downstream accepted the batch: a crash here re-sends at most this batch.
		return s.store.Set(ctx, ref, checkpoint.EntryState{Records: lastRecord})
	})
	if err != nil {
		return fmt.Errorf("%s!%s: %w", zipPath, e.Name, err)
	}
	return s.store.Set(ctx, ref, checkpoint.EntryState{Records: total, Done: true})
}

func matchesAny(patterns []string, name string, osPath bool) bool {
	for _, p := range patterns {
		var ok bool
		if osPath {
			ok, _ = doublestar.PathMatch(p, name)
		} else {
			ok, _ = doublestar.Match(p, name)
		}
		if ok {
			return true
		}
	}
	return false
}
