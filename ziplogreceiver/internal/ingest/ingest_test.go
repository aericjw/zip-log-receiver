package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/unicode"

	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/checkpoint"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/zipscan"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/split"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/trim"
)

type memPersister struct{ m map[string][]byte }

func (p *memPersister) Get(_ context.Context, k string) ([]byte, error) { return p.m[k], nil }
func (p *memPersister) Set(_ context.Context, k string, v []byte) error {
	p.m[k] = v
	return nil
}

func writeZip(t *testing.T, path string, entries [][2]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(e[1]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

type rec struct {
	body, zipName, entry string
}

type harness struct {
	cfg     Config
	persist *memPersister
	got     []rec
	// failAfter > 0 makes emit fail once that many batches were accepted.
	failAfter, accepted int
}

func newHarness(t *testing.T, include ...string) *harness {
	t.Helper()
	sf, err := split.Config{}.Func(unicode.UTF8, true, 1<<20)
	require.NoError(t, err)
	return &harness{
		persist: &memPersister{m: map[string][]byte{}},
		cfg: Config{
			Include:   include,
			Split:     sf,
			Trim:      trim.Whitespace,
			BatchSize: 2,
			MaxToken:  1 << 20,
		},
	}
}

func (h *harness) scan(t *testing.T) error {
	t.Helper()
	s := New(h.cfg, checkpoint.NewStore(h.persist), func(_ context.Context, tokens [][]byte, attrs map[string]any, _ int64) error {
		if h.failAfter > 0 && h.accepted >= h.failAfter {
			return errors.New("downstream unavailable")
		}
		h.accepted++
		for _, tk := range tokens {
			h.got = append(h.got, rec{string(tk), attrs["log.file.name"].(string), attrs["log.zip.entry"].(string)})
		}
		return nil
	})
	return s.Scan(context.Background())
}

func bodies(rs []rec) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.body
	}
	return out
}

func TestScanEmitsEveryEntryWithZipAndEntryAttributes(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"x.log", "1\n2\n3\n"}, {"sub/y.log", "4\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))

	require.NoError(t, h.scan(t))

	require.Equal(t, []rec{
		{"1", "a.zip", "x.log"}, {"2", "a.zip", "x.log"}, {"3", "a.zip", "x.log"},
		{"4", "a.zip", "sub/y.log"},
	}, h.got)
}

func TestSecondScanEmitsNothingBecauseEntriesAreDone(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"x.log", "1\n2\n3\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))
	require.NoError(t, h.scan(t))
	first := len(h.got)

	require.NoError(t, h.scan(t))

	require.Equal(t, first, len(h.got))
}

func TestFailedEmitResumesAfterLastCommittedBatchWithoutLossOrDuplication(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"x.log", "1\n2\n3\n4\n5\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))
	h.failAfter = 1 // accept batch [1,2], reject [3,4]

	require.Error(t, h.scan(t))
	require.Equal(t, []string{"1", "2"}, bodies(h.got))

	h.failAfter = 0 // downstream recovers
	require.NoError(t, h.scan(t))
	require.Equal(t, []string{"1", "2", "3", "4", "5"}, bodies(h.got))
}

func TestIncludeExcludeAndEntryFiltersSelectWhatIsRead(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "keep.zip"), [][2]string{{"a.log", "A\n"}, {"b.txt", "B\n"}})
	writeZip(t, filepath.Join(dir, "skip.zip"), [][2]string{{"c.log", "C\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))
	h.cfg.Exclude = []string{filepath.Join(dir, "skip.zip")}
	h.cfg.EntryInclude = []string{"**/*.log", "*.log"}
	h.cfg.EntryExclude = []string{"b.*"}

	require.NoError(t, h.scan(t))

	require.Equal(t, []string{"A"}, bodies(h.got))
}

func TestCorruptZipIsReportedButDoesNotStopOtherZips(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a-bad.zip"), []byte("not a zip"), 0o600))
	writeZip(t, filepath.Join(dir, "b-good.zip"), [][2]string{{"x.log", "ok\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))

	err := h.scan(t)

	require.ErrorContains(t, err, "a-bad.zip")
	require.Equal(t, []string{"ok"}, bodies(h.got))
}

func TestEntryOverSizeLimitIsReportedAndSkippedWhileOthersProceed(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"big.log", strings.Repeat("x\n", 500)}, {"small.log", "s\n"}})
	h := newHarness(t, filepath.Join(dir, "*.zip"))
	h.cfg.Limits = zipscan.Limits{MaxEntryUncompressedBytes: 100}

	err := h.scan(t)

	require.ErrorIs(t, err, zipscan.ErrLimitExceeded)
	require.Equal(t, []string{"s"}, bodies(h.got))
}
