package zipscan

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeZip creates a zip at dir/name containing the given entries (in order).
func writeZip(t *testing.T, dir, name string, entries [][2]string) string {
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
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	return p
}

func TestListEntriesReturnsFilesInArchiveOrderSkippingDirectories(t *testing.T) {
	dir := t.TempDir()
	p := writeZip(t, dir, "logs.zip", [][2]string{
		{"app/a.log", "one\ntwo\n"},
		{"app/", ""}, // directory entry
		{"app/b.log", "three\n"},
	})

	entries, err := ListEntries(p)
	require.NoError(t, err)

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	require.Equal(t, []string{"app/a.log", "app/b.log"}, names)
	require.EqualValues(t, len("one\ntwo\n"), entries[0].UncompressedSize)
}

func TestOpenEntryStreamsContent(t *testing.T) {
	p := writeZip(t, t.TempDir(), "logs.zip", [][2]string{{"a.log", "one\ntwo\n"}})

	rc, err := OpenEntry(p, "a.log", Limits{})
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, "one\ntwo\n", string(got))
}

func TestOpenEntryRejectsDeclaredSizeOverLimit(t *testing.T) {
	p := writeZip(t, t.TempDir(), "logs.zip", [][2]string{{"big.log", strings.Repeat("x", 1000)}})

	_, err := OpenEntry(p, "big.log", Limits{MaxEntryUncompressedBytes: 100})
	require.ErrorIs(t, err, ErrLimitExceeded)
}

func TestOpenEntryRejectsCompressionRatioOverLimit(t *testing.T) {
	// Highly compressible data: ratio far above 10.
	p := writeZip(t, t.TempDir(), "bomb.zip", [][2]string{{"z.log", strings.Repeat("a", 1<<20)}})

	_, err := OpenEntry(p, "z.log", Limits{MaxCompressionRatio: 10})
	require.ErrorIs(t, err, ErrLimitExceeded)
}

func TestOpenEntryErrorsWhenEntryMissing(t *testing.T) {
	p := writeZip(t, t.TempDir(), "logs.zip", [][2]string{{"a.log", "x"}})

	_, err := OpenEntry(p, "nope.log", Limits{})
	require.ErrorIs(t, err, ErrEntryNotFound)
}

func TestCapReaderFailsWhenSourceExceedsCapEvenIfHeaderLied(t *testing.T) {
	// 10 bytes of real data behind a cap of 4: simulates a header that under-declared the size.
	r := &capReader{r: strings.NewReader("0123456789"), remaining: 4}

	got, err := io.ReadAll(r)

	require.ErrorIs(t, err, ErrLimitExceeded)
	require.Equal(t, "0123", string(got))
}

func TestCapReaderAllowsExactlyCapBytes(t *testing.T) {
	r := &capReader{r: strings.NewReader("0123"), remaining: 4}

	got, err := io.ReadAll(r)

	require.NoError(t, err)
	require.Equal(t, "0123", string(got))
}
