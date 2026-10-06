package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func stubUploadSleep(t *testing.T) {
	t.Helper()
	old := uploadSleep
	uploadSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { uploadSleep = old })
}

func stubIPInfo(t *testing.T) {
	t.Helper()
	old := ipInfoClient
	ipInfoClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}
	t.Cleanup(func() { ipInfoClient = old })
}

func TestUploadToWorkerRetriesTransientHTTPFailures(t *testing.T) {
	stubUploadSleep(t)
	stubIPInfo(t)

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	rs := []Result{{IP: "104.16.1.1", Speed: 8.341}}
	n, err := UploadToWorker(context.Background(), WorkerTarget{URL: srv.URL, Token: "secret"}, rs, 1)
	if err != nil {
		t.Fatalf("want success after retries, got %v", err)
	}
	if n != 1 || attempts != 3 {
		t.Fatalf("n=%d attempts=%d", n, attempts)
	}
}

func TestUploadToWorkerStopsAfterMaxRetries(t *testing.T) {
	stubUploadSleep(t)
	stubIPInfo(t)

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	rs := []Result{{IP: "104.16.1.1", Speed: 8.341}}
	if _, err := UploadToWorker(context.Background(), WorkerTarget{URL: srv.URL, Token: "secret"}, rs, 1); err == nil {
		t.Fatal("want error after exhausting retries")
	}
	if want := uploadMaxRetries + 1; attempts != want {
		t.Fatalf("attempts=%d want %d", attempts, want)
	}
}

func TestUploadToWorkerDoesNotRetryClientErrors(t *testing.T) {
	stubUploadSleep(t)
	stubIPInfo(t)

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	rs := []Result{{IP: "104.16.1.1", Speed: 8.341}}
	if _, err := UploadToWorker(context.Background(), WorkerTarget{URL: srv.URL, Token: "secret"}, rs, 1); err == nil {
		t.Fatal("want error for HTTP 400")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1", attempts)
	}
}

func TestUploadToWorkerRetriesNetworkTimeout(t *testing.T) {
	stubUploadSleep(t)
	stubIPInfo(t)

	var attempts int
	oldClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
		}, nil
	})}
	t.Cleanup(func() { httpClient = oldClient })

	rs := []Result{{IP: "104.16.1.1", Speed: 8.341}}
	n, err := UploadToWorker(context.Background(), WorkerTarget{URL: "https://worker.example", Token: "secret"}, rs, 1)
	if err != nil {
		t.Fatalf("want success after retry, got %v", err)
	}
	if n != 1 || attempts != 3 {
		t.Fatalf("n=%d attempts=%d", n, attempts)
	}
}
