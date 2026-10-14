// Package metadata holds the component identity of the zip_log receiver.
package metadata

import "go.opentelemetry.io/collector/component"

var (
	// Type is the receiver type used in collector configs.
	Type = component.MustNewType("zip_log")
	// OperatorType is the stanza input operator type.
	OperatorType = "zip_input"
)

// LogsStability is the stability of the logs signal.
const LogsStability = component.StabilityLevelDevelopment
