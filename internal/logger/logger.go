package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/CoolBanHub/ailens360/internal/config"
)

func New(cfg config.LogConfig) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	var out io.Writer = os.Stderr
	if strings.ToLower(cfg.Format) == "json" {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	return slog.New(h)
}
