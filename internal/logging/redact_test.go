package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/tracewick/agent/internal/logging"
)

func TestRedactingHandlerScrubsSecretsAndKeys(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := logging.NewRedactingHandler(inner, "s3cret-value")
	log := slog.New(h)

	log.Info("using s3cret-value in message")
	log.Info("auth", "password", "s3cret-value", "user", "alice")
	log.Info("hdr", "Authorization", "Basic dXNlcjpzM2NyZXQtdmFsdWU=")
	log.With("token", "s3cret-value").Info("child")
	log.Info("nested", slog.Group("creds", slog.String("password", "s3cret-value"), slog.String("ok", "fine")))

	out := buf.String()
	if strings.Contains(out, "s3cret-value") {
		t.Fatalf("secret leaked:\n%s", out)
	}
	for _, want := range []string{"[redacted]", "alice", "fine"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("enabled")
	}
}
