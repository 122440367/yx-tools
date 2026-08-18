package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// APITarget 描述 cfnew 的优选 IP 接口位置
type APITarget struct {
	Domain string // Worker 域名，如 example.workers.dev
	UUID   string // UUID 或自定义路径
}

func (t APITarget) url() string {
	d := strings.TrimSpace(t.Domain)
	scheme := "https"
	// 允许显式指定 http，主要用于本地或内网自建
	if strings.HasPrefix(strings.ToLower(d), "http://") {
		scheme = "http"
		d = d[len("http://"):]
	} else if strings.HasPrefix(strings.ToLower(d), "https://") {
		d = d[len("https://"):]
	}
	d = strings.TrimSuffix(d, "/")
	// 去掉可能带上的路径部分
	if i := strings.Index(d, "/"); i >= 0 {
		d = d[:i]
	}
	u := strings.Trim(strings.TrimSpace(t.UUID), "/")
	return fmt.Sprintf("%s://%s/%s/api/preferred-ips", scheme, d, u)
}

type apiItem struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
	Name string `json:"name"`
}

// CountRemoteIPs 查询远端已有的优选 IP 数量
func CountRemoteIPs(ctx context.Context, t APITarget) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 160))
	}
	var out struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Count, nil
}

// ClearRemoteIPs 清空远端优选 IP
func ClearRemoteIPs(ctx context.Context, t APITarget) error {
	payload, _ := json.Marshal(map[string]bool{"all": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("清空失败 HTTP %d: %s", resp.StatusCode, truncate(string(body), 160))
	}
	return nil
}

// UploadToAPI 批量上报优选 IP 到 cfnew
func UploadToAPI(ctx context.Context, t APITarget, rs []Result, limit int, clear bool) (int, error) {
	if strings.TrimSpace(t.Domain) == "" || strings.TrimSpace(t.UUID) == "" {
		return 0, fmt.Errorf("请先填写 Worker 域名和 UUID")
	}
	if limit > 0 && limit < len(rs) {
		rs = rs[:limit]
	}
	if len(rs) == 0 {
		return 0, fmt.Errorf("没有可上传的结果")
	}
	if clear {
		if err := ClearRemoteIPs(ctx, t); err != nil {
			return 0, err
		}
	}
	items := make([]apiItem, 0, len(rs))
	for _, r := range rs {
		port := r.Port
		if port <= 0 {
			port = 443
		}
		items = append(items, apiItem{IP: r.IP, Port: port, Name: nodeName(r)})
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url(), bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("上传失败 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return len(items), nil
}

// nodeName 生成节点备注，如「香港-8.34MB/s」。
// 沿用旧 Python 版的格式：选优选 IP 看的是速度，延迟放名字里参考价值低。
func nodeName(r Result) string {
	name := ColoName(r.Colo)
	if name == "未知" {
		name = "未知地区"
	}
	return fmt.Sprintf("%s-%.2fMB/s", name, r.Speed)
}

// GitHubTarget 描述 GitHub 上传位置
type GitHubTarget struct {
	Repo  string // owner/repo
	Token string
	Path  string // 仓库内文件路径
}

// WorkerTarget describes a Worker endpoint that accepts the same text content
// generated for GitHub. Token must match the Worker's SPD_API_TOKEN secret.
type WorkerTarget struct {
	URL   string
	Token string
}

func (t WorkerTarget) url() (string, error) {
	u := strings.TrimSpace(t.URL)
	if u == "" {
		return "", fmt.Errorf("请先填写 Worker 地址")
	}
	if !strings.HasPrefix(strings.ToLower(u), "http://") && !strings.HasPrefix(strings.ToLower(u), "https://") {
		u = "https://" + u
	}
	u = strings.TrimRight(u, "/")
	if !strings.HasSuffix(u, "/upload-fast-ips") {
		u += "/upload-fast-ips"
	}
	return u, nil
}

// UploadToWorker sends results in exactly the same format as UploadToGitHub.
func UploadToWorker(ctx context.Context, t WorkerTarget, rs []Result, limit int) (int, error) {
	endpoint, err := t.url()
	if err != nil {
		return 0, err
	}
	token := strings.TrimSpace(t.Token)
	if token == "" {
		return 0, fmt.Errorf("请先填写 Worker API Token")
	}
	if limit > 0 && limit < len(rs) {
		rs = rs[:limit]
	}
	if len(rs) == 0 {
		return 0, fmt.Errorf("没有可上传的结果")
	}

	payload, err := json.Marshal(map[string]any{
		"content": buildGitHubContent(ctx, rs),
		"count":   len(rs),
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("Worker 上传失败 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return len(rs), nil
}

// TelegramTarget describes a Telegram Bot destination.
type TelegramTarget struct {
	BotToken string
	ChatID   string
}

var telegramAPIBase = "https://api.telegram.org"

// UploadToTelegram sends the selected speed-test results as one Telegram message.
func UploadToTelegram(ctx context.Context, t TelegramTarget, rs []Result, limit int) (int, error) {
	token, chatID := strings.TrimSpace(t.BotToken), strings.TrimSpace(t.ChatID)
	if token == "" || chatID == "" {
		return 0, fmt.Errorf("请先填写 Telegram Bot Token 和 Chat ID")
	}
	if limit > 0 && limit < len(rs) { rs = rs[:limit] }
	if len(rs) == 0 { return 0, fmt.Errorf("没有可发送的结果") }
	for _, message := range telegramMessages(rs) {
		payload, err := json.Marshal(map[string]string{"chat_id": chatID, "text": message})
		if err != nil { return 0, err }
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBase, token), bytes.NewReader(payload))
		if err != nil { return 0, err }
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil { return 0, err }
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 { return 0, fmt.Errorf("Telegram 发送失败 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200)) }
		var out struct { OK bool `json:"ok"`; Description string `json:"description"` }
		if err := json.Unmarshal(body, &out); err != nil || !out.OK {
			if out.Description == "" { out.Description = truncate(string(body), 200) }
			return 0, fmt.Errorf("Telegram 发送失败: %s", out.Description)
		}
	}
	return len(rs), nil
}

func telegramMessages(rs []Result) []string {
	header := fmt.Sprintf("yx-tools 测速结果（%d 个）\n\n", len(rs))
	messages := make([]string, 0, 1)
	var sb strings.Builder
	sb.WriteString(header)
	for i, r := range rs {
		line := fmt.Sprintf("%d. %s:%d  %.2f ms  %.2f MB/s  丢包 %.0f%%", i+1, r.IP, r.Port, r.Delay, r.Speed, r.LossRate*100)
		if r.ColoName != "" { line += "  " + r.ColoName }
		line += "\n"
		if sb.Len()+len(line) > 4096 && sb.Len() > 0 {
			messages = append(messages, strings.TrimSpace(sb.String()))
			sb.Reset()
		}
		sb.WriteString(line)
	}
	if sb.Len() > 0 { messages = append(messages, strings.TrimSpace(sb.String())) }
	return messages
}

const ipInfoBatchURL = "http://ip-api.com/batch?fields=status,query,countryCode,org,as,asname"

var ipInfoClient = &http.Client{Timeout: 10 * time.Second}

type ipInfo struct {
	CountryCode string
	Org         string
	AS          string
	ASName      string
}

type ipInfoResponse struct {
	Status      string `json:"status"`
	Query       string `json:"query"`
	CountryCode string `json:"countryCode"`
	Org         string `json:"org"`
	AS          string `json:"as"`
	ASName      string `json:"asname"`
}

// GitHub 文件在 Worker 的 niceip.txt 字段前增加从 1 开始的顺序编号，
// 但不再输出运营商栏。
// 国家和网络厂商优先查询，查询失败时仍会正常上传。
func buildGitHubContent(ctx context.Context, rs []Result) string {
	infos := batchLookupIPInfo(ctx, rs)
	return formatGitHubContent(rs, infos)
}

func batchLookupIPInfo(ctx context.Context, rs []Result) map[string]ipInfo {
	const batchSize = 100 // ip-api 免费批量接口单次最多 100 个查询
	infos := make(map[string]ipInfo, len(rs))
	seen := make(map[string]struct{}, len(rs))
	ips := make([]string, 0, len(rs))
	for _, r := range rs {
		key := normalizeIP(r.IP)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, key)
	}

	for start := 0; start < len(ips); start += batchSize {
		end := start + batchSize
		if end > len(ips) {
			end = len(ips)
		}
		queries := make([]map[string]string, 0, end-start)
		for _, ip := range ips[start:end] {
			queries = append(queries, map[string]string{"query": ip})
		}
		body, err := json.Marshal(queries)
		if err != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ipInfoBatchURL, bytes.NewReader(body))
		if err != nil {
			break
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := ipInfoClient.Do(req)
		if err != nil {
			break
		}
		var rows []ipInfoResponse
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rows)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			break
		}
		for _, row := range rows {
			if row.Status != "success" {
				continue
			}
			key := normalizeIP(row.Query)
			if key == "" {
				continue
			}
			infos[key] = ipInfo{
				CountryCode: row.CountryCode,
				Org:         row.Org,
				AS:          row.AS,
				ASName:      row.ASName,
			}
		}
	}
	return infos
}

func normalizeIP(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

func formatGitHubContent(rs []Result, infos map[string]ipInfo) string {
	var sb strings.Builder
	for i, r := range rs {
		ip := strings.TrimSpace(r.IP)
		info := infos[normalizeIP(ip)]
		country := strings.ToUpper(strings.TrimSpace(info.CountryCode))
		if len(country) != 2 {
			country = "XX"
		}
		provider := providerName(ip, info)
		uploadIP := formatUploadIP(ip, r.Port)
		fmt.Fprintf(&sb, "%s#%d | %s | %s | %sMB/s\n",
			uploadIP, i+1, country, provider, formatSpeed(r.Speed))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// formatUploadIP 保持 443 的兼容格式，非 443 端口写入 endpoint。
// IPv6 使用方括号，避免与端口分隔符混淆。
func formatUploadIP(ip string, port int) string {
	ip = strings.TrimSpace(ip)
	if port <= 0 || port == 443 {
		return ip
	}
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

func formatSpeed(speed float64) string {
	// GitHub 列表面向人阅读，统一保留两位小数，避免把测速计算中的
	// float64 二进制精度（例如 0.5390930239833219）直接暴露出来。
	return strconv.FormatFloat(speed, 'f', 2, 64)
}

var cloudflarePrefixes = mustPrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
)

func providerName(ip string, info ipInfo) string {
	sources := []string{info.Org, info.ASName, info.AS}
	joined := strings.ToLower(strings.Join(sources, " "))
	providers := []struct {
		needles []string
		name    string
	}{
		{[]string{"cloudflare", "cloudflarenet"}, "CF"},
		{[]string{"dmit"}, "DMIT"},
		{[]string{"amazon", "amazon-aes", "amazon-02", "aws"}, "AWS"},
		{[]string{"google", "google-cloud-platform"}, "GCP"},
		{[]string{"microsoft", "azure", "msn-as-block"}, "Azure"},
		{[]string{"hetzner"}, "Hetzner"},
		{[]string{"digitalocean"}, "DO"},
		{[]string{"vultr", "constant company", "choopa"}, "Vultr"},
		{[]string{"linode"}, "Linode"},
		{[]string{"akamai"}, "Akamai"},
		{[]string{"fastly"}, "Fastly"},
		{[]string{"ovh"}, "OVH"},
		{[]string{"m247"}, "M247"},
		{[]string{"frantech", "buyvm"}, "BuyVM"},
		{[]string{"psychz"}, "Psychz"},
		{[]string{"cogent"}, "Cogent"},
		{[]string{"gtt"}, "GTT"},
		{[]string{"ntt"}, "NTT"},
		{[]string{"telstra"}, "Telstra"},
		{[]string{"pccw"}, "PCCW"},
		{[]string{"chinanet", "china telecom"}, "ChinaNet"},
		{[]string{"china unicom", "china169"}, "CU"},
		{[]string{"china mobile", "cmnet"}, "CM"},
	}
	for _, p := range providers {
		for _, needle := range p.needles {
			if strings.Contains(joined, needle) {
				return p.name
			}
		}
	}
	if isCloudflareIP(ip) {
		return "CF"
	}

	// 没命中内置缩写时，保留接口返回的真实组织名，而不是笼统写成“其他”。
	org := firstNonEmpty(info.Org, info.ASName, trimASN(info.AS))
	if org == "" {
		return "未知"
	}
	parts := strings.FieldsFunc(org, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '/'
	})
	if len(parts) == 0 {
		return "未知"
	}
	word := parts[0]
	if len([]rune(word)) > 8 {
		return string([]rune(word)[:8])
	}
	return word
}

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		out = append(out, netip.MustParsePrefix(value))
	}
	return out
}

func isCloudflareIP(ip string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range cloudflarePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func trimASN(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) > 1 && strings.HasPrefix(strings.ToUpper(parts[0]), "AS") {
		return strings.Join(parts[1:], " ")
	}
	return value
}

// UploadToGitHub 把优选列表写入 GitHub 仓库，已存在则更新
func UploadToGitHub(ctx context.Context, t GitHubTarget, rs []Result, limit int) (int, error) {
	repo := strings.Trim(strings.TrimSpace(t.Repo), "/")
	if repo == "" || strings.TrimSpace(t.Token) == "" {
		return 0, fmt.Errorf("请先填写 GitHub 仓库和 Token")
	}
	if !strings.Contains(repo, "/") {
		return 0, fmt.Errorf("仓库格式应为 owner/repo")
	}
	path := strings.TrimSpace(t.Path)
	if path == "" {
		path = "cloudflare_ips.txt"
	}
	if limit > 0 && limit < len(rs) {
		rs = rs[:limit]
	}
	if len(rs) == 0 {
		return 0, fmt.Errorf("没有可上传的结果")
	}

	content := base64.StdEncoding.EncodeToString([]byte(buildGitHubContent(ctx, rs)))
	api := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repo, path)

	// 已存在则需要带上 sha 才能更新
	sha := ""
	{
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
		req.Header.Set("Authorization", "Bearer "+t.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		if resp, err := httpClient.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var meta struct {
					SHA string `json:"sha"`
				}
				b, _ := io.ReadAll(resp.Body)
				_ = json.Unmarshal(b, &meta)
				sha = meta.SHA
			}
		}
	}

	payload := map[string]string{
		"message": fmt.Sprintf("更新优选 IP (%d 个) %s", len(rs), time.Now().Format("2006-01-02 15:04")),
		"content": content,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, api, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("GitHub 上传失败 HTTP %d: %s", resp.StatusCode, truncate(string(rb), 200))
	}
	return len(rs), nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
