package pyroscope

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

const (
	defaultTimeout    = 10 * time.Second
	maxErrorBodyBytes = 4 << 10
)

// Config configures an HTTP Push client.
type Config struct {
	// ServerURL is the Pyroscope base URL (e.g. http://pyroscope:4040), without the Push path.
	ServerURL string
	// TenantID is sent as X-Scope-OrgID when non-empty.
	TenantID string
	// BasicUser and BasicPassword enable HTTP Basic auth. Both must be set together.
	BasicUser     string
	BasicPassword string
	// HTTPClient overrides the default client (timeouts / transport). Optional.
	HTTPClient *http.Client
	// NewSampleID overrides UUID generation in tests. Optional.
	NewSampleID func() (string, error)
}

// Client posts PushRequest bodies to Grafana Pyroscope.
type Client struct {
	endpoint    string
	tenantID    string
	basicAuth   string // already "Basic …", empty if unused
	httpClient  *http.Client
	newSampleID func() (string, error)
}

// NewClient validates cfg and returns a Push client.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("pyroscope: config is required")
	}
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("pyroscope: server URL is required")
	}
	base, err := url.Parse(cfg.ServerURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("pyroscope: server URL must be absolute http or https, got %q", cfg.ServerURL)
	}
	if (cfg.BasicUser == "") != (cfg.BasicPassword == "") {
		return nil, fmt.Errorf("pyroscope: basic authentication needs both a user and a password")
	}
	if strings.Contains(cfg.BasicUser, ":") {
		return nil, fmt.Errorf("pyroscope: basic authentication user must not contain a colon")
	}

	endpoint := strings.TrimRight(cfg.ServerURL, "/") + Path
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultTimeout,
			// Match httputil.NewHTTPClient: never follow redirects into a false 2xx ACK.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	newID := cfg.NewSampleID
	if newID == nil {
		newID = NewSampleID
	}

	c := &Client{
		endpoint:    endpoint,
		tenantID:    cfg.TenantID,
		httpClient:  httpClient,
		newSampleID: newID,
	}
	if cfg.BasicUser != "" {
		token := base64.StdEncoding.EncodeToString([]byte(cfg.BasicUser + ":" + cfg.BasicPassword))
		c.basicAuth = "Basic " + token
	}
	return c, nil
}

// Endpoint returns the absolute Push URL.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Profile is one pprof upload: application/event/labels from the spool header plus raw pprof bytes.
type Profile struct {
	Application string
	EventType   spool.EventType
	Labels      map[string]string
	RawProfile  []byte
	// SampleID is optional; when empty the client generates a UUID v4.
	SampleID string
}

// StatusError is a non-2xx HTTP response from Pyroscope.
type StatusError struct {
	StatusCode int
	Body       string
	// RetryAfter is the parsed Retry-After delay when the header is present and valid; zero otherwise.
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("pyroscope: push HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("pyroscope: push HTTP %d: %s", e.StatusCode, e.Body)
}

// Push encodes and POSTs one profile. Callers ack the spool claim only after a nil error (HTTP 2xx).
func (c *Client) Push(ctx context.Context, p *Profile) error {
	if p == nil {
		return fmt.Errorf("pyroscope: profile is required")
	}
	sampleIDValue := p.SampleID
	if sampleIDValue == "" {
		id, err := c.newSampleID()
		if err != nil {
			return err
		}
		sampleIDValue = id
	}
	body, err := Encode(p.Application, p.EventType, p.Labels, p.RawProfile, sampleIDValue)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pyroscope: build request: %w", err)
	}
	req.Header.Set("Content-Type", MediaType)
	if c.basicAuth != "" {
		req.Header.Set("Authorization", c.basicAuth)
	}
	if c.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", c.tenantID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pyroscope: push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return &StatusError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(snippet)),
		RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
	}
}

// ParseRetryAfter parses an HTTP Retry-After header (delay-seconds or HTTP-date).
// Invalid or empty values yield 0. now is used for HTTP-date relative delays.
func ParseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if sec < 0 {
			return 0
		}
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
