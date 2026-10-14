// Package zipscan lists and streams the file entries of zip archives.
package zipscan

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrEntryNotFound is returned when the requested entry is not in the archive.
	ErrEntryNotFound = errors.New("zip entry not found")
	// ErrLimitExceeded is returned when an entry violates a safety limit.
	ErrLimitExceeded = errors.New("zip entry exceeds safety limit")
)

// Limits guards against zip bombs. A zero value disables the corresponding check.
type Limits struct {
	// MaxEntryUncompressedBytes caps the uncompressed size of one entry.
	MaxEntryUncompressedBytes uint64
	// MaxCompressionRatio caps uncompressed/compressed size of one entry.
	MaxCompressionRatio float64
}

// Entry describes one file inside a zip archive.
type Entry struct {
	Name             string
	UncompressedSize uint64
	CRC32            uint32
}

// ListEntries returns the file (non-directory) entries of the zip at path, in archive order.
func ListEntries(path string) ([]Entry, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", path, err)
	}
	defer zr.Close()

	entries := make([]Entry, 0, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		entries = append(entries, Entry{
			Name:             f.Name,
			UncompressedSize: f.UncompressedSize64,
			CRC32:            f.CRC32,
		})
	}
	return entries, nil
}

// OpenEntry opens the named entry of the zip at path for streaming. The declared size and
// ratio are checked up front; the size cap is also enforced while reading because declared
// sizes in a zip header can be wrong. Closing the returned reader releases the archive.
func OpenEntry(path, name string, lim Limits) (io.ReadCloser, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", path, err)
	}
	for _, f := range zr.File {
		if f.Name != name || f.FileInfo().IsDir() {
			continue
		}
		if err := checkDeclared(f, lim); err != nil {
			zr.Close()
			return nil, fmt.Errorf("%s!%s: %w", path, name, err)
		}
		rc, err := f.Open()
		if err != nil {
			zr.Close()
			return nil, fmt.Errorf("open entry %s!%s: %w", path, name, err)
		}
		var r io.Reader = rc
		if lim.MaxEntryUncompressedBytes > 0 {
			r = &capReader{r: rc, remaining: lim.MaxEntryUncompressedBytes}
		}
		return &entryReader{Reader: r, closers: []io.Closer{rc, zr}}, nil
	}
	zr.Close()
	return nil, fmt.Errorf("%s!%s: %w", path, name, ErrEntryNotFound)
}

func checkDeclared(f *zip.File, lim Limits) error {
	if lim.MaxEntryUncompressedBytes > 0 && f.UncompressedSize64 > lim.MaxEntryUncompressedBytes {
		return fmt.Errorf("declared size %d > max %d: %w", f.UncompressedSize64, lim.MaxEntryUncompressedBytes, ErrLimitExceeded)
	}
	if lim.MaxCompressionRatio > 0 && f.CompressedSize64 > 0 {
		if ratio := float64(f.UncompressedSize64) / float64(f.CompressedSize64); ratio > lim.MaxCompressionRatio {
			return fmt.Errorf("compression ratio %.1f > max %.1f: %w", ratio, lim.MaxCompressionRatio, ErrLimitExceeded)
		}
	}
	return nil
}

// capReader fails with ErrLimitExceeded once more than remaining bytes could be read.
type capReader struct {
	r         io.Reader
	remaining uint64
}

func (c *capReader) Read(p []byte) (int, error) {
	if uint64(len(p)) > c.remaining+1 {
		p = p[:c.remaining+1] // read one extra byte so overflow is detectable
	}
	n, err := c.r.Read(p)
	if uint64(n) > c.remaining {
		return int(c.remaining), ErrLimitExceeded
	}
	c.remaining -= uint64(n)
	return n, err
}

type entryReader struct {
	io.Reader
	closers []io.Closer
}

func (e *entryReader) Close() error {
	var errs []error
	for _, c := range e.closers {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}
