package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchRemoteIPList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "yx-tools/") {
			t.Fatalf("unexpected User-Agent %q", got)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("192.0.2.1\n198.51.100.0/24\n203.0.113.2:8443\n"))
	}))
	defer srv.Close()

	got, err := fetchRemoteIPList(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "203.0.113.2:8443") {
		t.Fatalf("unexpected body %q", got)
	}
}

func TestFetchRemoteIPListRejectsChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fetchRemoteIPList(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "Cloudflare Challenge") {
		t.Fatalf("expected challenge error, got %v", err)
	}
}

func TestFetchRemoteIPListRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Just a moment...</html>"))
	}))
	defer srv.Close()

	_, err := fetchRemoteIPList(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "验证页面") {
		t.Fatalf("expected HTML error, got %v", err)
	}
}

func TestHTTPURLDetection(t *testing.T) {
	for _, value := range []string{"https://example.com/source.txt", "HTTP://example.com/source"} {
		if !isHTTPURL(value) {
			t.Fatalf("expected URL: %q", value)
		}
	}
	if isHTTPURL("./list.txt") {
		t.Fatal("local path must not be treated as URL")
	}
}
