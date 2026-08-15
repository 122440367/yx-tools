package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadToWorkerUsesGitHubFormatAndLimit(t *testing.T) {
	oldClient := ipInfoClient
	ipInfoClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}
	defer func() { ipInfoClient = oldClient }()

	var gotAuth, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload-fast-ips" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Content string `json:"content"`
			Count   int    `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Count != 2 {
			t.Fatalf("want count 2, got %d", body.Count)
		}
		gotContent = body.Content
		_, _ = w.Write([]byte(`{"success":true,"count":2}`))
	}))
	defer srv.Close()

	rs := []Result{
		{IP: "104.16.1.1", Speed: 8.341},
		{IP: "1.1.1.1", Speed: 5},
		{IP: "8.8.8.8", Speed: 3},
	}
	n, err := UploadToWorker(context.Background(), WorkerTarget{URL: srv.URL, Token: "secret"}, rs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || gotAuth != "Bearer secret" {
		t.Fatalf("count=%d auth=%q", n, gotAuth)
	}
	lines := strings.Split(gotContent, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %q", gotContent)
	}
	if lines[0] != "104.16.1.1#1 | XX | CF | 8.34MB/s" || lines[1] != "1.1.1.1#2 | XX | 未知 | 5.00MB/s" {
		t.Fatalf("unexpected content:\n%s", gotContent)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
