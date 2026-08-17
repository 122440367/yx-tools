package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func withFeishuServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	oldBase, oldClient, oldDelays, oldSleep := feishuAPIBase, feishuHTTPClient, feishuRetryDelays, feishuSleep
	feishuAPIBase = server.URL
	feishuHTTPClient = server.Client()
	feishuRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	feishuSleep = sleepContext
	t.Cleanup(func() {
		feishuAPIBase, feishuHTTPClient = oldBase, oldClient
		feishuRetryDelays, feishuSleep = oldDelays, oldSleep
	})
}

func TestValidateFeishuTarget(t *testing.T) {
	valid := FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "chat"}
	if err := ValidateFeishuTarget(valid); err != nil {
		t.Fatalf("valid target: %v", err)
	}
	for _, typ := range []string{"chat_id", "open_id", "union_id", "user_id", "email"} {
		t.Run(typ, func(t *testing.T) {
			target := valid
			target.ReceiveIDType = typ
			if err := ValidateFeishuTarget(target); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, target := range map[string]FeishuTarget{
		"missing secret":   {AppID: "app", ReceiveID: "chat"},
		"invalid type":     {AppID: "app", AppSecret: "secret", ReceiveID: "chat", ReceiveIDType: "phone"},
		"multiple targets": {AppID: "app", AppSecret: "secret", ReceiveID: "a,b"},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateFeishuTarget(target) == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFormatTaskSummaryIsAggregateAndRedacted(t *testing.T) {
	start := time.Date(2026, 8, 16, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	secret := "super-secret"
	long := secret + " " + strings.Repeat("错", 350) + " 1.2.3.4:443"
	got := FormatTaskSummary(TaskSummary{
		Operation: "test", Status: "failed", StartedAt: start, EndedAt: start.Add(2*time.Minute + 13*time.Second),
		ResultCount: 4, TestStatus: "success", UploadMode: "github", UploadStatus: "failed", UploadCount: 2,
		Failure: long,
	}, secret)
	localStart := start.In(time.Local)
	for _, want := range []string{
		"开始时间: " + localStart.Format("2006-01-02 15:04:05 MST"),
		"结束时间: " + localStart.Add(2*time.Minute+13*time.Second).Format("2006-01-02 15:04:05 MST"),
		"耗时: 2分13秒", "测速结果: 4 条", "上传: github（失败，2 条）", "[REDACTED]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, secret) {
		t.Fatal("secret leaked")
	}
	failure := strings.TrimPrefix(got[strings.LastIndex(got, "失败原因: "):], "失败原因: ")
	if len([]rune(failure)) > maxFailureRunes+3 {
		t.Fatalf("failure too long: %d runes", len([]rune(failure)))
	}
}

func TestNotifyFeishuSuccessAndReceiveTypes(t *testing.T) {
	for _, typ := range []string{"chat_id", "open_id", "union_id", "user_id", "email"} {
		t.Run(typ, func(t *testing.T) {
			var message map[string]string
			withFeishuServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/open-apis/auth/v3/tenant_access_token/internal":
					_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token"}`))
				case "/open-apis/im/v1/messages":
					if got := r.URL.Query().Get("receive_id_type"); got != typ {
						t.Errorf("receive type=%q", got)
					}
					if r.Header.Get("Authorization") != "Bearer tenant-token" {
						t.Error("missing bearer token")
					}
					if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
						t.Error(err)
					}
					_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			err := NotifyFeishu(context.Background(), FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "receiver", ReceiveIDType: typ}, TaskSummary{Operation: "upload", Status: "success", ResultCount: -1})
			if err != nil {
				t.Fatal(err)
			}
			if message["receive_id"] != "receiver" || message["msg_type"] != "text" || message["uuid"] == "" {
				t.Fatalf("bad payload: %#v", message)
			}
		})
	}
}

func TestNotifyFeishuRetriesWithStableIdempotency(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	var ids []string
	withFeishuServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "auth") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token"}`))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		requests++
		ids = append(ids, r.Header.Get("X-Idempotency-Key"))
		if requests < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("later"))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	if err := NotifyFeishu(context.Background(), FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "chat"}, TaskSummary{Operation: "test", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 || ids[0] == "" || ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("requests=%d ids=%v", requests, ids)
	}
}

func TestNotifyFeishuRetryLimitAndErrors(t *testing.T) {
	for name, response := range map[string]string{
		"malformed": "not-json",
		"platform":  `{"code":999,"msg":"bad secret-value"}`,
	} {
		t.Run(name, func(t *testing.T) {
			withFeishuServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "auth") {
					_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token"}`))
					return
				}
				_, _ = w.Write([]byte(response))
			}))
			err := NotifyFeishu(context.Background(), FeishuTarget{AppID: "app", AppSecret: "secret-value", ReceiveID: "chat"}, TaskSummary{})
			if err == nil || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("error=%v", err)
			}
		})
	}

	attempts := 0
	withFeishuServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "auth") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token"}`))
			return
		}
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	if err := NotifyFeishu(context.Background(), FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "chat"}, TaskSummary{}); err == nil {
		t.Fatal("expected retry exhaustion")
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestNotifyFeishuCancellationAndAmbiguousTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	withFeishuServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("request should not arrive") }))
	if err := NotifyFeishu(ctx, FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "chat"}, TaskSummary{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}

	attempts := 0
	oldClient := feishuHTTPClient
	feishuHTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, timeoutError{}
	})}
	t.Cleanup(func() { feishuHTTPClient = oldClient })
	err := NotifyFeishu(context.Background(), FeishuTarget{AppID: "app", AppSecret: "secret", ReceiveID: "chat"}, TaskSummary{})
	if err == nil || attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type timeoutError struct{}

func (timeoutError) Error() string   { return "ambiguous timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
