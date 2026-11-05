// Package logging provides slog helpers for the agent process.
package logging

import (
	"context"
	"log/slog"
	"slices"
	"strings"
)

var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "authorization", "auth", "api_key", "apikey",
}

// RedactingHandler wraps an inner handler and redacts secret attribute values.
type RedactingHandler struct {
	inner   slog.Handler
	secrets []string // non-empty secret substrings to scrub from string values
}

// NewRedactingHandler returns a handler that never logs configured secrets.
// secrets should include the Pyroscope password and any derived Basic token material.
func NewRedactingHandler(inner slog.Handler, secrets ...string) *RedactingHandler {
	clean := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			clean = append(clean, s)
		}
	}
	return &RedactingHandler{inner: inner, secrets: clean}
}

// Enabled reports whether the inner handler is enabled for level.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts attrs then forwards to the inner handler.
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error { //nolint:gocritic // slog.Handler requires Record by value
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, redactAttr(a, h.secrets))
		return true
	})
	out := slog.NewRecord(r.Time, r.Level, scrubString(r.Message, h.secrets), r.PC)
	out.AddAttrs(attrs...)
	return h.inner.Handle(ctx, out)
}

// WithAttrs returns a child handler with redacted attrs attached.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a, h.secrets)
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(out), secrets: h.secrets}
}

// WithGroup returns a child handler that groups attributes under name.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name), secrets: h.secrets}
}

func redactAttr(a slog.Attr, secrets []string) slog.Attr {
	a.Value = a.Value.Resolve()
	key := strings.ToLower(a.Key)
	if slices.ContainsFunc(sensitiveKeys, func(s string) bool { return key == s || strings.Contains(key, s) }) {
		return slog.String(a.Key, "[redacted]")
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, scrubString(a.Value.String(), secrets))
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, ga := range group {
			out[i] = redactAttr(ga, secrets)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	default:
		return a
	}
}

func scrubString(s string, secrets []string) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	out := s
	for _, secret := range secrets {
		if secret != "" && strings.Contains(out, secret) {
			out = strings.ReplaceAll(out, secret, "[redacted]")
		}
	}
	return out
}
