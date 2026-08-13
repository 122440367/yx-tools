package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

// GitHub 文件沿用 Worker 的 niceip.txt 格式。国家和网络组织优先查询，
// 查询失败时仍会正常上传，只是未知字段回退为 XX / 未知。
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
	for _, r := range rs {
		ip := strings.TrimSpace(r.IP)
		info := infos[normalizeIP(ip)]
		country := strings.ToUpper(strings.TrimSpace(info.CountryCode))
		if len(country) != 2 {
			country = "XX"
		}
		providerSource := info.Org
		if strings.TrimSpace(providerSource) == "" {
			providerSource = info.ASName
		}
		provider := providerAbbreviation(providerSource)
		isp := detectISP(ip, strings.Join([]string{info.Org, info.AS, info.ASName}, " "))
		fmt.Fprintf(&sb, "%s # %s | %s | %sMB/s | %s\n",
			ip, country, provider, formatSpeed(r.Speed), isp)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func formatSpeed(speed float64) string {
	return strconv.FormatFloat(speed, 'f', -1, 64)
}

func providerAbbreviation(org string) string {
	org = strings.TrimSpace(org)
	if org == "" {
		return "未知"
	}
	lower := strings.ToLower(org)
	providers := []struct {
		needle string
		name   string
	}{
		{"cloudflare", "CF"},
		{"dmit", "DMIT"},
		{"amazon", "AWS"},
		{"google", "GCP"},
		{"microsoft", "Azure"},
		{"hetzner", "Hetzner"},
		{"digitalocean", "DO"},
		{"vultr", "Vultr"},
		{"constant company", "Vultr"},
		{"linode", "Linode"},
		{"akamai", "Akamai"},
		{"fastly", "Fastly"},
		{"ovh", "OVH"},
		{"m247", "M247"},
		{"frantech", "BuyVM"},
		{"buyvm", "BuyVM"},
		{"psychz", "Psychz"},
		{"cogent", "Cogent"},
		{"gtt", "GTT"},
		{"ntt", "NTT"},
		{"telstra", "Telstra"},
		{"pccw", "PCCW"},
		{"chinanet", "ChinaNet"},
		{"china telecom", "ChinaNet"},
		{"china unicom", "CU"},
		{"china mobile", "CM"},
	}
	for _, p := range providers {
		if strings.Contains(lower, p.needle) {
			return p.name
		}
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

const (
	ispTelecom = "电信"
	ispUnicom  = "联通"
	ispMobile  = "移动"
	ispOther   = "其他"
)

var (
	telecomASNs = map[int]struct{}{4134: {}, 4809: {}, 4812: {}, 23764: {}}
	unicomASNs  = map[int]struct{}{4837: {}, 9929: {}, 10099: {}}
	mobileASNs  = map[int]struct{}{9808: {}, 24400: {}, 56040: {}, 58453: {}, 58807: {}}

	telecomPrefixes = mustPrefixes(
		"58.32.0.0/11", "59.32.0.0/11", "202.96.0.0/11", "219.128.0.0/11",
	)
	unicomPrefixes = mustPrefixes(
		"58.16.0.0/12", "60.0.0.0/11", "106.32.0.0/11", "175.0.0.0/11", "182.32.0.0/11",
	)
	mobilePrefixes = mustPrefixes(
		"36.32.0.0/11", "39.0.0.0/11", "183.0.0.0/11", "211.136.0.0/13", "218.200.0.0/13",
	)
)

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		out = append(out, netip.MustParsePrefix(value))
	}
	return out
}

// detectISP 优先使用 ASN/网络组织信息；只有查询不到时才使用不重叠的网段兜底。
// Worker 原来的 111.x—125.x 判断存在大量交叉，不能直接照搬，否则同一 IP
// 会因为判断顺序被错误归到电信。
func detectISP(ip, network string) string {
	lower := strings.ToLower(network)
	switch {
	case containsAny(lower, "china telecom", "chinatelecom", "chinanet", "ctgnet", "cn2"):
		return ispTelecom
	case containsAny(lower, "china unicom", "chinaunicom", "china169", "unicom"):
		return ispUnicom
	case containsAny(lower, "china mobile", "chinamobile", "cmnet", "cmi international", "tietong", "railcom"):
		return ispMobile
	}
	for _, token := range strings.FieldsFunc(strings.ToUpper(network), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !strings.HasPrefix(token, "AS") {
			continue
		}
		asn, err := strconv.Atoi(strings.TrimPrefix(token, "AS"))
		if err != nil {
			continue
		}
		if _, ok := telecomASNs[asn]; ok {
			return ispTelecom
		}
		if _, ok := unicomASNs[asn]; ok {
			return ispUnicom
		}
		if _, ok := mobileASNs[asn]; ok {
			return ispMobile
		}
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.Is4() {
		return ispOther
	}
	for _, p := range telecomPrefixes {
		if p.Contains(addr) {
			return ispTelecom
		}
	}
	for _, p := range unicomPrefixes {
		if p.Contains(addr) {
			return ispUnicom
		}
	}
	for _, p := range mobilePrefixes {
		if p.Contains(addr) {
			return ispMobile
		}
	}
	return ispOther
}

func containsAny(s string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(s, value) {
			return true
		}
	}
	return false
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
