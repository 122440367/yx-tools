package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/122440367/yx-tools/internal/app"
)

func TestResolveNotificationRequiresExplicitOptInAndSecret(t *testing.T) {
	fs := bindNotificationFlags(testFlagSet())
	cfg := app.DefaultConfig()
	if enabled, _, err := resolveNotification(fs, cfg); enabled || err != nil {
		t.Fatalf("empty notification should be disabled: %v", err)
	}
	*fs.mode = "feishu"
	*fs.appID, *fs.receiveID = "app", "chat"
	if _, _, err := resolveNotification(fs, cfg); err == nil {
		t.Fatal("App Secret must be supplied per invocation")
	}
	*fs.appSecret = "secret"
	if enabled, target, err := resolveNotification(fs, cfg); !enabled || err != nil || target.AppSecret != "secret" {
		t.Fatalf("target=%#v enabled=%v err=%v", target, enabled, err)
	}
}

func TestExecuteTestZeroResultsAndNoImplicitNotify(t *testing.T) {
	oldRun, oldWrite, oldNotify := runSpeedTest, writeCSV, notifyFeishu
	t.Cleanup(func() { runSpeedTest, writeCSV, notifyFeishu = oldRun, oldWrite, oldNotify })
	runSpeedTest = func(context.Context, app.Options, func(app.Progress)) ([]app.Result, error) { return nil, nil }
	writeCSV = func(string, []app.Result) error { return nil }
	notified := false
	notifyFeishu = func(context.Context, app.FeishuTarget, app.TaskSummary, ...string) error { notified = true; return nil }
	err := executeTest(context.Background(), []string{"-o", "result.csv"})
	if err == nil || err.Error() != "测速结束但没有有效结果" {
		t.Fatalf("zero-result error=%v", err)
	}
	if notified {
		t.Fatal("notification must not be sent without explicit opt-in")
	}
}

func TestExecuteTestNotificationFailurePreservesPrimaryOutcome(t *testing.T) {
	oldRun, oldNotify := runSpeedTest, notifyFeishu
	t.Cleanup(func() { runSpeedTest, notifyFeishu = oldRun, oldNotify })
	runSpeedTest = func(context.Context, app.Options, func(app.Progress)) ([]app.Result, error) { return nil, nil }
	var summary app.TaskSummary
	notifyFeishu = func(_ context.Context, target app.FeishuTarget, got app.TaskSummary, secrets ...string) error {
		summary = got
		if target.AppSecret != "never-store-me" {
			t.Fatalf("secret=%q", target.AppSecret)
		}
		return errors.New("notification unavailable")
	}
	err := executeTest(context.Background(), []string{"-notify", "feishu", "-feishu-app-id", "app", "-feishu-app-secret", "never-store-me", "-feishu-receive-id", "chat"})
	if err == nil || !strings.Contains(err.Error(), "没有有效结果") || !strings.Contains(err.Error(), "飞书通知失败") {
		t.Fatalf("combined error=%v", err)
	}
	if summary.Status != "failed" || summary.ResultCount != 0 || summary.StartedAt.IsZero() || summary.EndedAt.IsZero() {
		t.Fatalf("summary=%#v", summary)
	}
}

func testFlagSet() *flag.FlagSet { return flag.NewFlagSet("test", flag.ContinueOnError) }
