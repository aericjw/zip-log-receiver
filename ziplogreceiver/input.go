package ziplogreceiver

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/helper"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/trim"

	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/checkpoint"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/enc"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/ingest"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/zipscan"
)

const batchSize = 100

// Build constructs the zip input operator.
func (c InputConfig) Build(set component.TelemetrySettings) (operator.Operator, error) {
	base, err := c.InputConfig.Build(set)
	if err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	e, err := enc.Lookup(c.Encoding)
	if err != nil {
		return nil, err
	}
	splitFunc, err := c.Multiline.FuncWithLogger(e, true, int(c.MaxLogSize), set.Logger)
	if err != nil {
		return nil, err
	}

	trimFn := trim.Config{
		PreserveLeading:  c.PreserveLeadingWhitespaces,
		PreserveTrailing: c.PreserveTrailingWhitespaces,
	}.Func()
	utf8 := e == unicode.UTF8 || e == encoding.Nop
	scanTrim := trimFn
	if !utf8 {
		scanTrim = nil // raw bytes in a wide encoding can't be trimmed safely; trim after decoding
	}

	in := &Input{InputOperator: base, pollInterval: c.PollInterval, enc: e, utf8: utf8, trim: trimFn}
	in.scanCfg = ingest.Config{
		Include:      c.Include,
		Exclude:      c.Exclude,
		EntryInclude: c.EntryInclude,
		EntryExclude: c.EntryExclude,
		Limits: zipscan.Limits{
			MaxEntryUncompressedBytes: uint64(c.MaxEntryUncompressedBytes),
			MaxCompressionRatio:       c.MaxCompressionRatio,
		},
		Split:     splitFunc,
		Trim:      scanTrim,
		BatchSize: batchSize,
		MaxToken:  int(c.MaxLogSize),
	}
	return in, nil
}

// Input is the stanza input operator that reads log records from inside zip files.
type Input struct {
	helper.InputOperator

	scanCfg      ingest.Config
	pollInterval time.Duration
	enc          encoding.Encoding
	utf8         bool
	trim         trim.Func

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Start begins scanning in the background. Progress is kept in memory and, when the persister is
// backed by a storage extension, on disk too, so finished entries are never re-read.
func (i *Input) Start(persister operator.Persister) error {
	ctx, cancel := context.WithCancel(context.Background())
	i.cancel = cancel
	store := checkpoint.NewStore(checkpoint.NewCached(persister))
	scanner := ingest.New(i.scanCfg, store, i.emit)

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		t := time.NewTicker(i.pollInterval)
		defer t.Stop()
		for {
			if err := scanner.Scan(ctx); err != nil && ctx.Err() == nil {
				i.Logger().Error("zip scan finished with errors", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return nil
}

// Stop stops scanning and waits for the scan loop to exit.
func (i *Input) Stop() error {
	if i.cancel != nil {
		i.cancel()
	}
	i.wg.Wait()
	return nil
}

func (i *Input) emit(ctx context.Context, tokens [][]byte, attrs map[string]any, _ int64) error {
	entries := make([]*entry.Entry, 0, len(tokens))
	for _, tok := range tokens {
		body, ok := i.decode(tok)
		if !ok {
			continue
		}
		ent, err := i.NewEntry(body)
		if err != nil {
			i.Logger().Error("create entry", zap.Error(err))
			continue
		}
		// Default the event time to the observed time; operators (e.g. time_parser) can overwrite it.
		ent.Timestamp = ent.ObservedTimestamp
		for k, v := range attrs {
			if err := ent.Set(entry.NewAttributeField(k), v); err != nil {
				i.Logger().Error("set attribute", zap.String("key", k), zap.Error(err))
			}
		}
		entries = append(entries, ent)
	}
	return i.WriteBatch(ctx, entries)
}

// decode turns a raw token into the record body. ok is false for tokens that are empty after trimming.
func (i *Input) decode(tok []byte) (body any, ok bool) {
	if i.enc == encoding.Nop {
		return tok, true
	}
	if !i.utf8 {
		dec, err := i.enc.NewDecoder().Bytes(tok)
		if err != nil {
			i.Logger().Warn("decode token", zap.Error(err))
			return nil, false
		}
		tok = i.trim(dec)
		if len(tok) == 0 {
			return nil, false
		}
	}
	return string(tok), true
}
