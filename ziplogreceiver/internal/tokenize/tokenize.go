// Package tokenize splits a byte stream into log records and hands them to a callback in batches.
package tokenize

import (
	"bufio"
	"io"
)

const defaultMaxToken = 1 << 20

// Options controls how a stream is tokenized.
type Options struct {
	// Split decides where records begin and end (lines, multiline patterns, encodings).
	Split bufio.SplitFunc
	// Trim post-processes each token (e.g. strip whitespace and CR). Nil keeps tokens as-is.
	Trim func([]byte) []byte
	// BatchSize is the max records per emit call; values < 1 are treated as 1.
	BatchSize int
	// Skip is the number of leading records to drop without emitting (resume support).
	Skip int64
	// MaxToken bounds the scanner buffer; <= 0 means 1 MiB.
	MaxToken int
}

// EmitFunc receives a batch of records and the running record number of the last one.
// The tokens are owned by the callee.
type EmitFunc func(tokens [][]byte, lastRecord int64) error

// Tokenize reads r to EOF, emitting non-empty records in batches. It returns the total number
// of records seen, including skipped ones. Empty tokens are dropped and not counted, so record
// numbers are stable across resumes. It stops at the first emit or read error.
func Tokenize(r io.Reader, opts Options, emit EmitFunc) (int64, error) {
	max := opts.MaxToken
	if max <= 0 {
		max = defaultMaxToken
	}
	batchSize := opts.BatchSize
	if batchSize < 1 {
		batchSize = 1
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 16*1024), max)
	sc.Split(opts.Split)

	var (
		total int64
		batch = make([][]byte, 0, batchSize)
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		out := batch
		batch = make([][]byte, 0, batchSize)
		return emit(out, total)
	}

	for sc.Scan() {
		tok := sc.Bytes()
		if opts.Trim != nil {
			tok = opts.Trim(tok)
		}
		if len(tok) == 0 {
			continue
		}
		total++
		if total <= opts.Skip {
			continue
		}
		batch = append(batch, append([]byte(nil), tok...)) // scanner reuses its buffer
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return total, err
	}
	return total, flush()
}
