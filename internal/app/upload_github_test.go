package app

import "testing"

func TestFormatGitHubContentMatchesWorkerFormat(t *testing.T) {
	rs := []Result{
		{IP: "1.1.1.1", Port: 443, Speed: 8.34},
		{IP: "202.100.1.1", Port: 8443, Speed: 5},
		{IP: "2001:db8::1", Port: 2053, Speed: 3.5},
	}
	infos := map[string]ipInfo{
		"1.1.1.1":     {CountryCode: "AU", Org: "Cloudflare, Inc.", AS: "AS13335 Cloudflare, Inc."},
		"2001:db8::1": {CountryCode: "CN", Org: "China Mobile", AS: "AS9808 China Mobile"},
	}
	want := "1.1.1.1 # AU | CF | 8.34MB/s | 其他\n" +
		"202.100.1.1 # XX | 未知 | 5MB/s | 电信\n" +
		"2001:db8::1 # CN | CM | 3.5MB/s | 移动"
	if got := formatGitHubContent(rs, infos); got != want {
		t.Fatalf("want:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestDetectISPUsesNetworkMetadataBeforePrefixFallback(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		network string
		want    string
	}{
		{"电信组织名", "8.8.8.8", "CHINANET-BACKBONE No.31,Jin-rong Street", ispTelecom},
		{"联通ASN", "8.8.8.8", "AS4837 CHINA UNICOM China169 Backbone", ispUnicom},
		{"移动ASN", "2001:db8::1", "AS9808 China Mobile Communications Group", ispMobile},
		{"电信网段兜底", "202.100.1.1", "", ispTelecom},
		{"联通网段兜底", "58.20.1.1", "", ispUnicom},
		{"移动网段兜底", "211.140.1.1", "", ispMobile},
		{"不照搬重叠大网段", "111.1.1.1", "", ispOther},
		{"普通Cloudflare地址", "1.1.1.1", "AS13335 Cloudflare", ispOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectISP(tc.ip, tc.network); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestProviderAbbreviation(t *testing.T) {
	cases := map[string]string{
		"Cloudflare, Inc.": "CF",
		"China Telecom":    "ChinaNet",
		"China Unicom":     "CU",
		"China Mobile":     "CM",
		"":                 "未知",
	}
	for input, want := range cases {
		if got := providerAbbreviation(input); got != want {
			t.Fatalf("providerAbbreviation(%q): want %q, got %q", input, want, got)
		}
	}
}
