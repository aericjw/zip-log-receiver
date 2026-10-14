// Package ziplogreceiver reads log records from inside zip archives, like a Splunk Universal
// Forwarder reads archived logs, and feeds them through the stanza operator pipeline.
package ziplogreceiver

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/adapter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"

	"github.com/aericjw/zip-log-receiver/ziplogreceiver/internal/metadata"
)

// NewFactory creates the zip_log receiver factory.
func NewFactory() receiver.Factory {
	return adapter.NewFactory(ReceiverType{}, metadata.LogsStability)
}

// ReceiverType implements adapter.LogReceiverType.
type ReceiverType struct{}

// Type is the receiver type.
func (ReceiverType) Type() component.Type { return metadata.Type }

// CreateDefaultConfig creates the default config.
func (ReceiverType) CreateDefaultConfig() component.Config {
	return &Config{
		BaseConfig:  adapter.BaseConfig{Operators: []operator.Config{}},
		InputConfig: *NewInputConfig(),
	}
}

// BaseConfig returns the shared stanza settings.
func (ReceiverType) BaseConfig(cfg component.Config) adapter.BaseConfig {
	return cfg.(*Config).BaseConfig
}

// InputConfig returns the zip input operator config.
func (ReceiverType) InputConfig(cfg component.Config) operator.Config {
	return operator.NewConfig(&cfg.(*Config).InputConfig)
}
