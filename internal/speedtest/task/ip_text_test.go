package task

import "testing"

func TestIPTextSupportsLinesCommentsAndPorts(t *testing.T) {
	oldText, oldFile, oldAll, oldSample := IPText, IPFile, TestAll, SampleSize
	oldPorts := PortMapping
	defer func() {
		IPText, IPFile, TestAll, SampleSize = oldText, oldFile, oldAll, oldSample
		PortMapping = oldPorts
	}()

	IPText = "192.0.2.1\n198.51.100.0/24 # comment\n203.0.113.2:8443\n[2001:db8::1]:2053"
	IPFile = ""
	TestAll = false
	SampleSize = 0
	PortMapping = make(map[string]int)

	ips := loadIPRanges()
	if len(ips) != 4 {
		t.Fatalf("want 4 candidates, got %d", len(ips))
	}
	if PortMapping["203.0.113.2"] != 8443 || PortMapping["2001:db8::1"] != 2053 {
		t.Fatalf("unexpected port mapping: %#v", PortMapping)
	}
}
