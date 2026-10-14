package checkpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// nopPersister forgets everything, like the stanza adapter's default when no storage extension is set.
type nopPersister struct{}

func (nopPersister) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (nopPersister) Set(context.Context, string, []byte) error   { return nil }

func TestCachedRemembersWritesEvenWhenUnderlyingForgets(t *testing.T) {
	c := NewCached(nopPersister{})
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", []byte("v")))
	got, err := c.Get(ctx, "k")

	require.NoError(t, err)
	require.Equal(t, []byte("v"), got)
}

func TestCachedWritesThroughToUnderlying(t *testing.T) {
	under := newMem()
	c := NewCached(under)

	require.NoError(t, c.Set(context.Background(), "k", []byte("v")))

	require.Equal(t, []byte("v"), under.m["k"])
}

func TestCachedReadsFallThroughToUnderlyingForUnseenKeys(t *testing.T) {
	under := newMem()
	under.m["old"] = []byte("from-disk") // e.g. state persisted before a restart
	c := NewCached(under)

	got, err := c.Get(context.Background(), "old")

	require.NoError(t, err)
	require.Equal(t, []byte("from-disk"), got)
}

func TestCachedDoesNotRememberAWriteTheUnderlyingRejected(t *testing.T) {
	c := NewCached(&failingPersister{*newMem()})
	ctx := context.Background()

	err := c.Set(ctx, "k", []byte("v"))
	got, getErr := c.Get(ctx, "k")

	require.Error(t, err)
	require.NoError(t, getErr)
	require.Nil(t, got)
}

type errGetPersister struct{}

func (errGetPersister) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("read fail")
}
func (errGetPersister) Set(context.Context, string, []byte) error { return nil }

func TestCachedPropagatesUnderlyingReadError(t *testing.T) {
	_, err := NewCached(errGetPersister{}).Get(context.Background(), "k")

	require.ErrorContains(t, err, "read fail")
}
