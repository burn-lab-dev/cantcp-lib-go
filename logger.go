package cantcp

import (
	"context"
	"log/slog"
)

// LevelTrace is the logging level used for per-packet tracing.
// It is lower than slog.LevelDebug so tracing can be enabled on its own.
const LevelTrace = slog.Level(-8)

// Logger is the minimal logging interface used by the parser.
// *slog.Logger implements it directly, so no adapter is needed.
// A nil Logger disables logging completely.
type Logger interface {
	Log(ctx context.Context, level slog.Level, msg string, args ...any)
}
