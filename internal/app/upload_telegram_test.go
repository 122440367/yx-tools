package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadToTelegram(t *testing.T) {
	var gotChatID, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret/sendMessage" { t.Fatalf("unexpected path %q", r.URL.Path) }
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
		gotChatID, gotText = body["chat_id"], body["text"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	oldBase := telegramAPIBase
	telegramAPIBase = srv.URL
	defer func() { telegramAPIBase = oldBase }()

	rs := []Result{{IP: "1.1.1.1", Port: 443, Delay: 12.34, Speed: 8.5, ColoName: "香港"}}
	n, err := UploadToTelegram(context.Background(), TelegramTarget{BotToken: "secret", ChatID: "-100123"}, rs, 0)
	if err != nil { t.Fatal(err) }
	if n != 1 || gotChatID != "-100123" { t.Fatalf("n=%d chat_id=%q", n, gotChatID) }
	if !strings.Contains(gotText, "1.1.1.1:443") || !strings.Contains(gotText, "8.50 MB/s") { t.Fatalf("text=%q", gotText) }
}

func TestTelegramMessagesSplitAtLimit(t *testing.T) {
	rs := make([]Result, 150)
	for i := range rs { rs[i] = Result{IP: "2001:db8::1234", Port: 443, ColoName: strings.Repeat("区", 20)} }
	messages := telegramMessages(rs)
	if len(messages) < 2 { t.Fatalf("expected multiple messages, got %d", len(messages)) }
	for _, message := range messages {
		if len(message) > 4096 { t.Fatalf("message has %d bytes", len(message)) }
	}
}
