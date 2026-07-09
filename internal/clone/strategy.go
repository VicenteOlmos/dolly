package clone

import (
	"context"
	"errors"
	"fmt"
)

// Strategy executes a complete clone operation.
type Strategy interface {
	Execute(ctx context.Context, opts Options) error
	Name() string
}

// ErrStrategyGuided is returned when a strategy intentionally defers execution
// and provides guidance instead (e.g. physical-backup replication).
var ErrStrategyGuided = errors.New("strategy requires manual setup; see guidance message")

// Resolve returns the concrete Strategy for a strategy name.
// An empty string resolves to the default "schema-replay" strategy.
func Resolve(strategyName string, opts Options) (Strategy, error) {
	name := strategyName
	if name == "" {
		name = "schema-replay"
	}

	switch name {
	case "template":
		return &TemplateStrategy{}, nil
	case "schema-replay":
		return &SchemaReplayStrategy{
			Runner: opts.CommandRunner,
		}, nil
	case "logical-stream", "copy-stream", "streaming-copy":
		return &CopyStreamStrategy{
			Runner: opts.CommandRunner,
		}, nil
	case "physical-backup", "replication":
		return &ReplicationStrategy{Runner: opts.CommandRunner}, nil
	default:
		return nil, fmt.Errorf("unknown clone strategy %q; supported: template, schema-replay, logical-stream, physical-backup", strategyName)
	}
}
