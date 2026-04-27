package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseExpires(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		in   string
		want string
		err  bool
	}{
		{"30 days", "30d", "2026-05-27T12:00:00Z", false},
		{"12 hours", "12h", "2026-04-28T00:00:00Z", false},
		{"5 minutes", "5m", "2026-04-27T12:05:00Z", false},
		{"60 seconds", "60s", "2026-04-27T12:01:00Z", false},
		{"rfc3339 utc", "2026-12-31T00:00:00Z", "2026-12-31T00:00:00Z", false},
		{"rfc3339 with offset", "2026-12-31T00:00:00+03:00", "2026-12-30T21:00:00Z", false},
		{"empty", "", "", true},
		{"unknown word", "forever", "", true},
		{"zero duration", "0d", "", true},
		{"negative duration", "-5d", "", true},
		{"unknown unit", "30days", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExpires(tc.in, now)
			if tc.err {
				if err == nil {
					t.Fatalf("parseExpires(%q): want error, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExpires(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseExpires(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRunHappyPath(t *testing.T) {
	var seen createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/links" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("content-type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		if ua := r.Header.Get("user-agent"); !strings.HasPrefix(ua, "loqsu-cli/") {
			t.Errorf("user-agent = %q", ua)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			Slug:     "test",
			ShortURL: "http://" + r.Host + "/test",
		})
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	args := []string{"-s", srv.URL, "-a", "test", "-m", "100", "https://example.com"}
	if err := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr=%q)", err, stderr.String())
	}

	if seen.URL != "https://example.com" {
		t.Errorf("server saw url=%q", seen.URL)
	}
	if seen.Alias != "test" {
		t.Errorf("server saw alias=%q", seen.Alias)
	}
	if seen.MaxClicks == nil || *seen.MaxClicks != 100 {
		t.Errorf("server saw max_clicks=%v", seen.MaxClicks)
	}
	if got := strings.TrimSpace(stdout.String()); !strings.HasSuffix(got, "/test") {
		t.Errorf("stdout = %q, want suffix /test", got)
	}
}

func TestRunReadsStdin(t *testing.T) {
	var seen createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seen)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			Slug:     "x",
			ShortURL: "http://" + r.Host + "/x",
		})
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	args := []string{"-s", srv.URL, "-"}
	stdin := strings.NewReader("https://piped.example.com\n")
	if err := run(context.Background(), args, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if seen.URL != "https://piped.example.com" {
		t.Errorf("server saw url=%q", seen.URL)
	}
	if !strings.Contains(stdout.String(), "/x") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "target host is blocked: phishing"})
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"-s", srv.URL, "https://bad.example.com"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "phishing") {
		t.Errorf("error message lacks server reason: %v", err)
	}
}

func TestRunRejectsBadScheme(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"ftp://example.com"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errBadScheme) {
		t.Fatalf("err = %v, want errBadScheme", err)
	}
}

func TestRunMissingURL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), nil, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errMissingURL) {
		t.Fatalf("err = %v, want errMissingURL", err)
	}
}

func TestRunEmptyURLFromStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"-"}, strings.NewReader("\n"), &stdout, &stderr)
	if !errors.Is(err, errEmptyURL) {
		t.Fatalf("err = %v, want errEmptyURL", err)
	}
}

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"-V"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "loqsu ") {
		t.Errorf("stdout = %q, want loqsu prefix", stdout.String())
	}
}

func TestRunJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			Slug:     "abc",
			ShortURL: "http://" + r.Host + "/abc",
		})
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	args := []string{"-s", srv.URL, "-j", "https://example.com"}
	if err := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got["slug"] != "abc" {
		t.Errorf("slug = %v", got["slug"])
	}
}

func TestRunRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{"-s", srv.URL, "https://example.com"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// Guard: a giant response body must not be drained beyond maxRespBytes.
func TestSubmitRespectsBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"slug":"x","short_url":"http://x/x","target_url":"`+strings.Repeat("a", maxRespBytes*2)+`"}`)
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"-s", srv.URL, "https://example.com"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected decode error from truncated body, got nil")
	}
}
