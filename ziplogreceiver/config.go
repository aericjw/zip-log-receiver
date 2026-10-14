package ziplogreceiver

import (
	"errors"
	"fmt"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/adapter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/helper"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/split"

	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/enc"
	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/metadata"
)

const (
	defaultPollInterval = time.Second
	defaultMaxLogSize   = 1024 * 1024
)

func init() {
	operator.Register(metadata.OperatorType, func() operator.Builder { return NewInputConfig() })
}

// Config is the zip_log receiver configuration: the zip input operator plus the shared stanza
// receiver settings (operators, storage, retry_on_failure).
type Config struct {
	InputConfig        `mapstructure:",squash"`
	adapter.BaseConfig `mapstructure:",squash"`

	// prevent unkeyed literal initialization
	_ struct{}
}

// InputConfig configures the zip input operator.
type InputConfig struct {
	helper.InputConfig `mapstructure:",squash"`

	// Include and Exclude are globs (with **) over zip file paths.
	Include []string `mapstructure:"include"`
	Exclude []string `mapstructure:"exclude"`
	// EntryInclude and EntryExclude are globs over entry names inside a zip. Empty EntryInclude means all.
	EntryInclude []string `mapstructure:"entry_include"`
	EntryExclude []string `mapstructure:"entry_exclude"`

	// PollInterval is how often to look for new zips. Entries already ingested are skipped.
	PollInterval time.Duration `mapstructure:"poll_interval"`

	Encoding  string       `mapstructure:"encoding"`
	Multiline split.Config `mapstructure:"multiline"`
	// MaxLogSize is the max size of a single record.
	MaxLogSize                  helper.ByteSize `mapstructure:"max_log_size"`
	PreserveLeadingWhitespaces  bool            `mapstructure:"preserve_leading_whitespaces"`
	PreserveTrailingWhitespaces bool            `mapstructure:"preserve_trailing_whitespaces"`

	// Zip bomb guards; 0 disables a check.
	MaxEntryUncompressedBytes helper.ByteSize `mapstructure:"max_entry_uncompressed_bytes"`
	MaxCompressionRatio       float64         `mapstructure:"max_compression_ratio"`
}

// NewInputConfig returns the default zip input configuration.
func NewInputConfig() *InputConfig {
	return &InputConfig{
		InputConfig:  helper.NewInputConfig(metadata.OperatorType, metadata.OperatorType),
		PollInterval: defaultPollInterval,
		Encoding:     "utf-8",
		MaxLogSize:   defaultMaxLogSize,
	}
}

// Validate checks the configuration.
func (c *Config) Validate() error { return c.InputConfig.validate() }

func (c InputConfig) validate() error {
	var errs []error
	if len(c.Include) == 0 {
		errs = append(errs, errors.New("'include' must contain at least one zip file pattern"))
	}
	if c.PollInterval <= 0 {
		errs = append(errs, fmt.Errorf("'poll_interval' must be positive, got %s", c.PollInterval))
	}
	if c.MaxLogSize <= 0 {
		errs = append(errs, fmt.Errorf("'max_log_size' must be positive, got %d", c.MaxLogSize))
	}
	if c.MaxCompressionRatio < 0 {
		errs = append(errs, errors.New("'max_compression_ratio' must not be negative"))
	}
	if _, err := enc.Lookup(c.Encoding); err != nil {
		errs = append(errs, fmt.Errorf("'encoding': %w", err))
	}
	return errors.Join(errs...)
}
