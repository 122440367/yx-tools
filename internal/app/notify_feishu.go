package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	feishuNotifyTimeout = 10 * time.Second
	feishuMaxRetries    = 2
	maxFailureRunes     = 300
)

var (
	feishuAPIBase     = "https://open.feishu.cn"
	feishuHTTPClient  = &http.Client{Timeout: feishuNotifyTimeout}
	feishuRetryDelays = []time.Duration{500 * time.Millisecond, time.Second}
	feishuSleep       = sleepContext
)

// FeishuTarget identifies one receiver for an internal-app bot notification.
// AppSecret is deliberately request-scoped and is never part of Config.
type FeishuTarget struct {
	AppID         string
	AppSecret     string
	ReceiveID     string
	ReceiveIDType string
}

// TaskSummary is the aggregate task outcome sent to Feishu. It intentionally
// contains no individual speed-test result fields.
type TaskSummary struct {
	Operation    string
	Status       string
	StartedAt    time.Time
	EndedAt      time.Time
	ResultCount  int
	TestStatus   string
	WriteStatus  string
	UploadMode   string
	UploadStatus string
	UploadCount  int
	Failure      string
}

var validFeishuReceiveTypes = map[string]struct{}{
	"chat_id":  {},
	"open_id":  {},
	"union_id": {},
	"user_id":  {},
	"email":    {},
}

// ValidateFeishuTarget validates the effective single-receiver target.
func ValidateFeishuTarget(t FeishuTarget) error {
	missing := make([]string, 0, 3)
	if strings.TrimSpace(t.AppID) == "" {
		missing = append(missing, "App ID")
	}
	if strings.TrimSpace(t.AppSecret) == "" {
		missing = append(missing, "App Secret")
	}
	if strings.TrimSpace(t.ReceiveID) == "" {
		missing = append(missing, "Receive ID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少飞书配置: %s", strings.Join(missing, "、"))
	}
	typ := strings.TrimSpace(t.ReceiveIDType)
	if typ == "" {
		typ = "chat_id"
	}
	if _, ok := validFeishuReceiveTypes[typ]; !ok {
		return fmt.Errorf("不支持的飞书 receive_id_type: %s", typ)
	}
	if strings.ContainsAny(t.ReceiveID, ",;\r\n") {
		return errors.New("一次命令只能指定一个飞书 Receive ID")
	}
	return nil
}

// FormatTaskSummary renders the deterministic, aggregate-only notification.
func FormatTaskSummary(s TaskSummary, secrets ...string) string {
	ended := s.EndedAt
	if ended.IsZero() {
		ended = time.Now()
	}
	started := s.StartedAt
	if started.IsZero() {
		started = ended
	}
	duration := ended.Sub(started)
	if duration < 0 {
		duration = 0
	}
	lines := []string{
		"yx-tools 任务通知",
		"任务: " + displayOperation(s.Operation),
		"状态: " + displayStatus(s.Status),
		"开始时间: " + formatLocalTime(started),
		"结束时间: " + formatLocalTime(ended),
		"耗时: " + formatDuration(duration),
	}
	if s.Operation == "test" || s.ResultCount >= 0 {
		lines = append(lines, fmt.Sprintf("测速结果: %d 条", max(s.ResultCount, 0)))
	}
	if strings.TrimSpace(s.TestStatus) != "" {
		lines = append(lines, "测速: "+displayStatus(s.TestStatus))
	}
	if strings.TrimSpace(s.WriteStatus) != "" {
		lines = append(lines, "结果文件: "+displayStatus(s.WriteStatus))
	}
	if strings.TrimSpace(s.UploadMode) != "" {
		upload := s.UploadMode
		if strings.TrimSpace(s.UploadStatus) != "" {
			upload += "（" + displayStatus(s.UploadStatus)
			if s.UploadCount > 0 {
				upload += fmt.Sprintf("，%d 条", s.UploadCount)
			}
			upload += "）"
		}
		lines = append(lines, "上传: "+upload)
	}
	if failure := sanitizeFailure(s.Failure, secrets...); failure != "" {
		lines = append(lines, "失败原因: "+failure)
	}
	return strings.Join(lines, "\n")
}

func displayOperation(value string) string {
	switch value {
	case "test":
		return "测速"
	case "upload":
		return "上传"
	default:
		return value
	}
}

func displayStatus(value string) string {
	switch value {
	case "success":
		return "成功"
	case "failed":
		return "失败"
	case "cancelled":
		return "已取消"
	case "skipped":
		return "未执行"
	default:
		return value
	}
}

func formatLocalTime(t time.Time) string {
	return t.In(time.Local).Format("2006-01-02 15:04:05 MST")
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Second {
		return "不足1秒"
	}
	h := int(d / time.Hour)
	m := int(d%time.Hour) / int(time.Minute)
	s := int(d%time.Minute) / int(time.Second)
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d小时", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%d分", m))
	}
	if s > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d秒", s))
	}
	return strings.Join(parts, "")
}

func sanitizeFailure(value string, secrets ...string) string {
	value = strings.TrimSpace(value)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= maxFailureRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxFailureRunes]) + "..."
}

// NotifyFeishu authenticates and sends one idempotent text notification.
func NotifyFeishu(ctx context.Context, target FeishuTarget, summary TaskSummary, secrets ...string) error {
	if strings.TrimSpace(target.ReceiveIDType) == "" {
		target.ReceiveIDType = "chat_id"
	}
	if err := ValidateFeishuTarget(target); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feishuNotifyTimeout)
	defer cancel()

	var auth struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	authPayload := map[string]string{"app_id": target.AppID, "app_secret": target.AppSecret}
	if err := feishuJSON(ctx, http.MethodPost, feishuAPIBase+"/open-apis/auth/v3/tenant_access_token/internal", authPayload, "", "", &auth); err != nil {
		return fmt.Errorf("飞书鉴权失败: %w", err)
	}
	if auth.Code != 0 || strings.TrimSpace(auth.TenantAccessToken) == "" {
		return fmt.Errorf("飞书鉴权失败: code=%d msg=%s", auth.Code, sanitizeFailure(auth.Msg, target.AppSecret))
	}

	redactions := append([]string{}, secrets...)
	redactions = append(redactions, target.AppSecret, auth.TenantAccessToken)
	content, err := json.Marshal(map[string]string{
		"text": FormatTaskSummary(summary, redactions...),
	})
	if err != nil {
		return fmt.Errorf("编码飞书消息: %w", err)
	}
	uuid, err := randomID()
	if err != nil {
		return fmt.Errorf("生成飞书消息幂等标识: %w", err)
	}
	payload := map[string]string{
		"receive_id": target.ReceiveID,
		"msg_type":   "text",
		"content":    string(content),
		"uuid":       uuid,
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	messageURL := feishuAPIBase + "/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(target.ReceiveIDType)
	if err := feishuJSON(ctx, http.MethodPost, messageURL, payload, auth.TenantAccessToken, uuid, &out); err != nil {
		return fmt.Errorf("发送飞书通知: %w", err)
	}
	if out.Code != 0 {
		return fmt.Errorf("发送飞书通知: code=%d msg=%s", out.Code, sanitizeFailure(out.Msg, target.AppSecret, auth.TenantAccessToken))
	}
	return nil
}

func feishuJSON(ctx context.Context, method, url string, payload any, token, idempotencyKey string, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码请求: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt <= feishuMaxRetries; attempt++ {
		if attempt > 0 {
			delay := feishuRetryDelays[min(attempt-1, len(feishuRetryDelays)-1)]
			if retryErr, ok := lastErr.(*retryableHTTPError); ok && retryErr.retryAfter > 0 {
				delay = retryErr.retryAfter
			}
			if err := feishuSleep(ctx, delay); err != nil {
				return err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("创建请求: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if idempotencyKey != "" {
			req.Header.Set("X-Idempotency-Key", idempotencyKey)
		}
		resp, err := feishuHTTPClient.Do(req)
		if err != nil {
			lastErr = err
			if !isRetryableNetworkError(err) || attempt == feishuMaxRetries {
				return fmt.Errorf("请求失败: %w", err)
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("读取响应: %w", readErr)
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = &retryableHTTPError{status: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")), body: truncate(string(respBody), 200)}
			if attempt < feishuMaxRetries {
				continue
			}
			return lastErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("解析响应: %w", err)
		}
		return nil
	}
	return lastErr
}

type retryableHTTPError struct {
	status     int
	retryAfter time.Duration
	body       string
}

func (e *retryableHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

func isRetryableNetworkError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && !netErr.Timeout()
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
