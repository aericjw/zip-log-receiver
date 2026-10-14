package tokenize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/unicode"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/split"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/trim"
)

type batch struct {
	tokens     []string
	lastRecord int64
}

func run(t *testing.T, input string, sc split.Config, batchSize int, skip int64) ([]batch, int64) {
	t.Helper()
	splitFunc, err := sc.Func(unicode.UTF8, true, 1<<20)
	require.NoError(t, err)

	var got []batch
	total, err := Tokenize(strings.NewReader(input), Options{
		Split:     splitFunc,
		Trim:      trim.Whitespace,
		BatchSize: batchSize,
		Skip:      skip,
		MaxToken:  1 << 20,
	}, func(tokens [][]byte, lastRecord int64) error {
		b := batch{lastRecord: lastRecord}
		for _, tk := range tokens {
			b.tokens = append(b.tokens, string(tk))
		}
		got = append(got, b)
		return nil
	})
	require.NoError(t, err)
	return got, total
}

func TestTokenizeSplitsLinesIntoBatchesWithRunningRecordNumbers(t *testing.T) {
	got, total := run(t, "a\nb\nc\n", split.Config{}, 2, 0)

	require.Equal(t, []batch{{[]string{"a", "b"}, 2}, {[]string{"c"}, 3}}, got)
	require.EqualValues(t, 3, total)
}

func TestTokenizeTrimsCRLFAndHandlesMissingFinalNewline(t *testing.T) {
	got, total := run(t, "a\r\nb", split.Config{}, 10, 0)

	require.Equal(t, []batch{{[]string{"a", "b"}, 2}}, got)
	require.EqualValues(t, 2, total)
}

func TestTokenizeSkipsAlreadyEmittedRecordsOnResume(t *testing.T) {
	got, total := run(t, "a\nb\nc\nd\n", split.Config{}, 10, 2)

	require.Equal(t, []batch{{[]string{"c", "d"}, 4}}, got)
	require.EqualValues(t, 4, total)
}

func TestTokenizeCombinesMultilineRecords(t *testing.T) {
	in := "2026-01-01 boom\n  at foo\n  at bar\n2026-01-02 ok\n"
	got, total := run(t, in, split.Config{LineStartPattern: `^\d{4}-`}, 10, 0)

	require.Equal(t, []batch{{[]string{"2026-01-01 boom\n  at foo\n  at bar", "2026-01-02 ok"}, 2}}, got)
	require.EqualValues(t, 2, total)
}

func TestTokenizeDropsEmptyTokensWithoutCountingThem(t *testing.T) {
	got, total := run(t, "a\n\nb\n", split.Config{}, 10, 0)

	require.Equal(t, []batch{{[]string{"a", "b"}, 2}}, got)
	require.EqualValues(t, 2, total)
}

func TestTokenizeStopsAndReturnsEmitError(t *testing.T) {
	splitFunc, err := split.Config{}.Func(unicode.UTF8, true, 1<<20)
	require.NoError(t, err)
	boom := errBoom{}

	_, err = Tokenize(strings.NewReader("a\nb\n"), Options{Split: splitFunc, Trim: trim.Whitespace, BatchSize: 1, MaxToken: 1 << 20},
		func([][]byte, int64) error { return boom })

	require.ErrorIs(t, err, boom)
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
