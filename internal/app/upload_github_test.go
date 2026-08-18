package app

import "testing"

func TestFormatGitHubContentIncludesSequenceNumber(t *testing.T) {
	rs := []Result{
		{IP: "1.1.1.1", Port: 443, Speed: 8.34},
		{IP: "202.100.1.1", Port: 8443, Speed: 5},
		{IP: "2001:db8::1", Port: 2053, Speed: 3.5},
	}
	infos := map[string]ipInfo{
		"1.1.1.1":     {CountryCode: "AU", Org: "Cloudflare, Inc.", AS: "AS13335 Cloudflare, Inc."},
		"2001:db8::1": {CountryCode: "CN", Org: "Customer Name", AS: "AS9808 China Mobile", ASName: "CHINAMOBILE-CN"},
	}
	got := formatGitHubContent(rs, infos)
	want := "1.1.1.1#1 | AU | CF | 8.34MB/s\n" +
		"202.100.1.1:8443#2 | XX | 未知 | 5.00MB/s\n" +
		"[2001:db8::1]:2053#3 | CN | CM | 3.50MB/s"
	if got != want {
		t.Fatalf("want:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestFormatSpeedUsesTwoDecimalPlaces(t *testing.T) {
	if got, want := formatSpeed(0.5390930239833219), "0.54"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestProviderName(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		info ipInfo
		want string
	}{
		{"从org识别", "1.1.1.1", ipInfo{Org: "Cloudflare, Inc."}, "CF"},
		{"org是客户名时从asname识别", "8.8.8.8", ipInfo{Org: "Customer Name", ASName: "CLOUDFLARENET"}, "CF"},
		{"从AS描述识别", "8.8.8.8", ipInfo{Org: "Customer Name", AS: "AS16509 Amazon.com, Inc."}, "AWS"},
		{"Cloudflare IPv4离线兜底", "104.16.1.1", ipInfo{}, "CF"},
		{"Cloudflare IPv6离线兜底", "2606:4700::1", ipInfo{}, "CF"},
		{"未知厂商保留真实组织名", "8.8.8.8", ipInfo{Org: "Example Hosting Ltd"}, "Example"},
		{"完全没有信息", "8.8.8.8", ipInfo{}, "未知"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerName(tc.ip, tc.info); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
