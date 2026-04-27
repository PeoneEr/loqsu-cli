// Command loqsu is a command-line client for the loq.su URL shortener.
//
// Usage:
//
//	loqsu [flags] <url>
//	loqsu [flags] -          # read URL from stdin
//
// See `loqsu --help` for the full list of flags.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version is set at link time:
//
//	go build -ldflags "-X main.version=v0.1.0"
var version = "dev"

const (
	defaultServer = "https://loq.su"
	envServer     = "LOQSU_SERVER"
	httpTimeout   = 15 * time.Second
	maxRespBytes  = 64 * 1024
	maxStdinBytes = 4096
)

// Sentinel errors for tests and structured handling.
var (
	errMissingURL = errors.New("missing url argument")
	errEmptyURL   = errors.New("empty url")
	errBadScheme  = errors.New("url must start with http:// or https://")
)

type createRequest struct {
	URL       string  `json:"url"`
	Alias     string  `json:"alias,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	MaxClicks *int64  `json:"max_clicks,omitempty"`
}

type createResponse struct {
	Slug      string  `json:"slug"`
	ShortURL  string  `json:"short_url"`
	TargetURL string  `json:"target_url"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	MaxClicks *int64  `json:"max_clicks,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "loqsu: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var (
		alias     string
		expires   string
		maxClicks int64
		jsonOut   bool
		server    string
		showVer   bool
	)

	fs := flag.NewFlagSet("loqsu", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.StringVar(&alias, "alias", "", "custom slug, 3–32 chars, [a-zA-Z0-9_-]")
	fs.StringVar(&alias, "a", "", "shorthand for --alias")
	fs.StringVar(&expires, "expires", "", "expiration: RFC3339 (2026-12-31T00:00:00Z) or duration (30d, 12h, 5m)")
	fs.StringVar(&expires, "e", "", "shorthand for --expires")
	fs.Int64Var(&maxClicks, "max-clicks", 0, "max clicks before the link expires (0 = unlimited)")
	fs.Int64Var(&maxClicks, "m", 0, "shorthand for --max-clicks")
	fs.BoolVar(&jsonOut, "json", false, "print full JSON response instead of just the short URL")
	fs.BoolVar(&jsonOut, "j", false, "shorthand for --json")
	fs.StringVar(&server, "server", "", "API server URL (default: $LOQSU_SERVER or "+defaultServer+")")
	fs.StringVar(&server, "s", "", "shorthand for --server")
	fs.BoolVar(&showVer, "version", false, "print version and exit")
	fs.BoolVar(&showVer, "V", false, "shorthand for --version")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "loqsu — CLI for the loq.su URL shortener\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  loqsu [flags] <url>\n")
		fmt.Fprintf(stderr, "  loqsu [flags] -          read URL from stdin\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  loqsu https://example.com/very/long/path\n")
		fmt.Fprintf(stderr, "  loqsu -a my-promo https://example.com\n")
		fmt.Fprintf(stderr, "  loqsu -e 30d -m 100 https://example.com\n")
		fmt.Fprintf(stderr, "  echo https://example.com | loqsu -\n")
		fmt.Fprintf(stderr, "  loqsu --json https://example.com | jq .slug\n\n")
		fmt.Fprintf(stderr, "Environment:\n")
		fmt.Fprintf(stderr, "  LOQSU_SERVER       override the default API server\n")
		fmt.Fprintf(stderr, "\nSource: https://github.com/PeoneEr/loqsu-cli\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if showVer {
		fmt.Fprintln(stdout, "loqsu "+version)
		return nil
	}

	rawURL, err := pickURL(fs.Args(), stdin)
	if err != nil {
		if errors.Is(err, errMissingURL) {
			fs.Usage()
		}
		return err
	}

	if server == "" {
		server = os.Getenv(envServer)
	}
	if server == "" {
		server = defaultServer
	}
	server = strings.TrimRight(server, "/")

	body := createRequest{URL: rawURL, Alias: alias}
	if expires != "" {
		ts, err := parseExpires(expires, time.Now())
		if err != nil {
			return fmt.Errorf("invalid --expires: %w", err)
		}
		body.ExpiresAt = &ts
	}
	if maxClicks > 0 {
		body.MaxClicks = &maxClicks
	}

	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	return submit(reqCtx, server, body, jsonOut, stdout)
}

func pickURL(args []string, stdin io.Reader) (string, error) {
	switch {
	case len(args) == 0:
		return "", errMissingURL
	case len(args) == 1 && args[0] == "-":
		b, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return validateURL(strings.TrimSpace(string(b)))
	case len(args) == 1:
		return validateURL(strings.TrimSpace(args[0]))
	default:
		return "", fmt.Errorf("unexpected arguments: %v (pass exactly one url)", args[1:])
	}
}

func validateURL(s string) (string, error) {
	if s == "" {
		return "", errEmptyURL
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "", errBadScheme
	}
	return s, nil
}

func submit(ctx context.Context, server string, body createRequest, jsonOut bool, stdout io.Writer) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/links", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "loqsu-cli/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var er errorResponse
		if err := json.Unmarshal(raw, &er); err == nil && er.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, er.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	if jsonOut {
		if _, err := stdout.Write(raw); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return nil
	}

	var cr createResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Fprintln(stdout, cr.ShortURL)
	return nil
}

var durationRE = regexp.MustCompile(`^(\d+)\s*([dhms])$`)

// parseExpires accepts an RFC3339 timestamp or a short duration like
// "30d", "12h", "5m", "60s" and returns an RFC3339 timestamp in UTC.
func parseExpires(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}

	m := durationRE.FindStringSubmatch(s)
	if m == nil {
		return "", errors.New("expected RFC3339 (e.g. 2026-12-31T00:00:00Z) or duration (e.g. 30d, 12h)")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return "", errors.New("duration value must be a positive integer")
	}

	var d time.Duration
	switch m[2] {
	case "d":
		d = time.Duration(n) * 24 * time.Hour
	case "h":
		d = time.Duration(n) * time.Hour
	case "m":
		d = time.Duration(n) * time.Minute
	case "s":
		d = time.Duration(n) * time.Second
	}

	return now.UTC().Add(d).Format(time.RFC3339), nil
}
