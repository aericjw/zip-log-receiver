package enc

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding"
)

func decode(t *testing.T, name string, in []byte) string {
	t.Helper()
	e, err := Lookup(name)
	require.NoError(t, err)
	out, err := e.NewDecoder().Bytes(in)
	require.NoError(t, err)
	return string(out)
}

func TestLookupDefaultsToUTF8WhenEmpty(t *testing.T) {
	require.Equal(t, "héllo", decode(t, "", []byte("héllo")))
}

func TestLookupDecodesUTF16LEAndBE(t *testing.T) {
	require.Equal(t, "hi", decode(t, "utf-16le", []byte{'h', 0, 'i', 0}))
	require.Equal(t, "hi", decode(t, "utf-16be", []byte{0, 'h', 0, 'i'}))
}

func TestLookupIsCaseInsensitiveAndAcceptsIANANames(t *testing.T) {
	require.Equal(t, "é", decode(t, "ISO-8859-1", []byte{0xe9}))
	require.Equal(t, "hi", decode(t, "UTF-16LE", []byte{'h', 0, 'i', 0}))
}

func TestLookupNopReturnsNopEncodingSoCallersCanDetectIt(t *testing.T) {
	e, err := Lookup("nop")

	require.NoError(t, err)
	require.Equal(t, encoding.Nop, e)
}

func TestLookupRejectsUnknownEncoding(t *testing.T) {
	_, err := Lookup("klingon-9000")

	require.ErrorContains(t, err, "klingon-9000")
}
