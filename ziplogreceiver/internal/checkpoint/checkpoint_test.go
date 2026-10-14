package checkpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// memPersister is an in-memory Persister; Get on a missing key returns (nil, nil) like the real one.
type memPersister struct{ m map[string][]byte }

func newMem() *memPersister { return &memPersister{m: map[string][]byte{}} }

func (p *memPersister) Get(_ context.Context, k string) ([]byte, error) { return p.m[k], nil }
func (p *memPersister) Set(_ context.Context, k string, v []byte) error {
	p.m[k] = v
	return nil
}

var ref = EntryRef{ZipPath: "/data/a.zip", Entry: "app/x.log", CRC32: 0xdeadbeef, Size: 100}

func TestGetReturnsZeroStateForUnknownEntry(t *testing.T) {
	s := NewStore(newMem())

	st, err := s.Get(context.Background(), ref)

	require.NoError(t, err)
	require.Equal(t, EntryState{}, st)
}

func TestSetThenGetRoundTripsState(t *testing.T) {
	s := NewStore(newMem())
	want := EntryState{Records: 42, Done: true}

	require.NoError(t, s.Set(context.Background(), ref, want))
	got, err := s.Get(context.Background(), ref)

	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestStateIsIsolatedPerEntryIdentity(t *testing.T) {
	s := NewStore(newMem())
	ctx := context.Background()
	require.NoError(t, s.Set(ctx, ref, EntryState{Records: 5}))

	variants := map[string]EntryRef{
		"other zip path": {ZipPath: "/data/b.zip", Entry: ref.Entry, CRC32: ref.CRC32, Size: ref.Size},
		"other entry":    {ZipPath: ref.ZipPath, Entry: "app/y.log", CRC32: ref.CRC32, Size: ref.Size},
		"other crc":      {ZipPath: ref.ZipPath, Entry: ref.Entry, CRC32: 1, Size: ref.Size},
		"other size":     {ZipPath: ref.ZipPath, Entry: ref.Entry, CRC32: ref.CRC32, Size: 101},
	}
	for name, v := range variants {
		got, err := s.Get(ctx, v)
		require.NoError(t, err, name)
		require.Equal(t, EntryState{}, got, name)
	}
}

func TestGetReportsCorruptStoredValue(t *testing.T) {
	p := newMem()
	s := NewStore(p)
	p.m[s.key(ref)] = []byte("not json")

	_, err := s.Get(context.Background(), ref)

	require.Error(t, err)
}

type failingPersister struct{ memPersister }

func (failingPersister) Set(context.Context, string, []byte) error { return errors.New("disk full") }

func TestSetPropagatesPersisterError(t *testing.T) {
	s := NewStore(&failingPersister{*newMem()})

	require.ErrorContains(t, s.Set(context.Background(), ref, EntryState{}), "disk full")
}
