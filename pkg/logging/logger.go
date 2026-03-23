package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

var Logger zerolog.Logger

// Init initializes the global logger with the specified level and format.
func Init(level, format string) {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var logLevel zerolog.Level
	switch level {
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	default:
		logLevel = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(logLevel)
	// Force debug level for testing
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	var writer io.Writer = os.Stdout
	if format == "console" {
		writer = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	Logger = zerolog.New(writer).With().Timestamp().Caller().Logger()
}

// WithContext returns a logger with additional context fields.
func WithContext(ctx map[string]any) zerolog.Logger {
	l := Logger
	for k, v := range ctx {
		l = l.With().Interface(k, v).Logger()
	}
	return l
}
