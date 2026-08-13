package task

import (
	"bufio"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultInputFile = "ip.txt"

var (
	// TestAll test all ip
	TestAll = false
	// IPFile is the filename of IP Rangs
	IPFile = defaultInputFile
	IPText string
	// PortMapping stores IP to port mapping for proxy mode
	PortMapping = make(map[string]int)
	// SampleSize 限制参与延迟测速的候选 IP 数量，0 表示不限
	SampleSize = 0
)

func InitRandSeed() {
	rand.Seed(time.Now().UnixNano())
}

func isIPv4(ip string) bool {
	return strings.Contains(ip, ".")
}

func randIPEndWith(num byte) byte {
	if num == 0 { // 对于 /32 这种单独的 IP
		return byte(0)
	}
	return byte(rand.Intn(int(num)))
}

type IPRanges struct {
	ips     []*net.IPAddr
	mask    string
	firstIP net.IP
	ipNet   *net.IPNet
}

func newIPRanges() *IPRanges {
	return &IPRanges{
		ips: make([]*net.IPAddr, 0),
	}
}

// 如果是单独 IP 则加上子网掩码，反之则获取子网掩码(r.mask)
func (r *IPRanges) fixIP(ip string) string {
	// 如果不含有 '/' 则代表不是 IP 段，而是一个单独的 IP，因此需要加上 /32 /128 子网掩码
	if i := strings.IndexByte(ip, '/'); i < 0 {
		if isIPv4(ip) {
			r.mask = "/32"
		} else {
			r.mask = "/128"
		}
		ip += r.mask
	} else {
		r.mask = ip[i:]
	}
	return ip
}

// 解析 IP 段，获得 IP、IP 范围、子网掩码
func (r *IPRanges) parseCIDR(ip string) {
	var err error
	if r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip)); err != nil {
		fatalf("IP 段格式不对: %s", ip)
	}
}

func (r *IPRanges) appendIPv4(d byte) {
	r.appendIP(net.IPv4(r.firstIP[12], r.firstIP[13], r.firstIP[14], d))
}

func (r *IPRanges) appendIP(ip net.IP) {
	r.ips = append(r.ips, &net.IPAddr{IP: ip})
}

// 返回第四段 ip 的最小值及可用数目
func (r *IPRanges) getIPRange() (minIP, hosts byte) {
	minIP = r.firstIP[15] & r.ipNet.Mask[3] // IP 第四段最小值

	// 根据子网掩码获取主机数量
	m := net.IPv4Mask(255, 255, 255, 255)
	for i, v := range r.ipNet.Mask {
		m[i] ^= v
	}
	total, _ := strconv.ParseInt(m.String(), 16, 32) // 总可用 IP 数
	if total > 255 {                                 // 矫正 第四段 可用 IP 数
		hosts = 255
		return
	}
	hosts = byte(total)
	return
}

func (r *IPRanges) chooseIPv4() {
	if r.mask == "/32" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP)
	} else {
		minIP, hosts := r.getIPRange()    // 返回第四段 IP 的最小值及可用数目
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			if TestAll { // 如果是测速全部 IP
				for i := 0; i <= int(hosts); i++ { // 遍历 IP 最后一段最小值到最大值
					r.appendIPv4(byte(i) + minIP)
				}
			} else { // 随机 IP 的最后一段 0.0.0.X
				r.appendIPv4(minIP + randIPEndWith(hosts))
			}
			r.firstIP[14]++ // 0.0.(X+1).X
			if r.firstIP[14] == 0 {
				r.firstIP[13]++ // 0.(X+1).X.X
				if r.firstIP[13] == 0 {
					r.firstIP[12]++ // (X+1).X.X.X
				}
			}
		}
	}
}

func (r *IPRanges) chooseIPv6() {
	if r.mask == "/128" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP)
	} else {
		var tempIP uint8                  // 临时变量，用于记录前一位的值
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			r.firstIP[15] = randIPEndWith(255) // 随机 IP 的最后一段
			r.firstIP[14] = randIPEndWith(255) // 随机 IP 的最后一段

			targetIP := make([]byte, len(r.firstIP))
			copy(targetIP, r.firstIP)
			r.appendIP(targetIP) // 加入 IP 地址池

			for i := 13; i >= 0; i-- { // 从倒数第三位开始往前随机
				tempIP = r.firstIP[i]              // 保存前一位的值
				r.firstIP[i] += randIPEndWith(255) // 随机 0~255，加到当前位上
				if r.firstIP[i] >= tempIP {        // 如果当前位的值大于等于前一位的值，说明随机成功了，可以退出该循环
					break
				}
			}
		}
	}
}

func loadIPRanges() []*net.IPAddr {
	ranges := newIPRanges()
	if IPText != "" { // 从参数中获取 IP 段数据
		loadIPScanner(ranges, bufio.NewScanner(strings.NewReader(strings.ReplaceAll(IPText, ",", "\n"))))
	} else { // 从文件中获取 IP 段数据
		if IPFile == "" {
			IPFile = defaultInputFile
		}
		file, err := os.Open(IPFile)
		if err != nil {
			fatalf("打开 IP 文件失败: %v", err)
		}
		defer file.Close()
		loadIPScanner(ranges, bufio.NewScanner(file))
	}
	return sampleIPs(ranges.ips)
}

func loadIPScanner(ranges *IPRanges, scanner *bufio.Scanner) {
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		line = parsePort(line)
		ranges.parseCIDR(line)
		if isIPv4(line) {
			ranges.chooseIPv4()
		} else {
			ranges.chooseIPv6()
		}
	}
	if err := scanner.Err(); err != nil {
		fatalf("读取 IP 列表失败: %v", err)
	}
}

func parsePort(line string) string {
	if strings.HasPrefix(line, "[") {
		if host, portText, err := net.SplitHostPort(line); err == nil {
			if port, err := strconv.Atoi(portText); err == nil && port > 0 && port < 65536 && net.ParseIP(host) != nil {
				PortMapping[host] = port
				return host
			}
		}
	}
	if strings.Count(line, ":") == 1 && !strings.Contains(line, "/") {
		if host, portText, err := net.SplitHostPort(line); err == nil {
			if port, err := strconv.Atoi(portText); err == nil && port > 0 && port < 65536 && net.ParseIP(host) != nil {
				PortMapping[host] = port
				return host
			}
		}
	}
	return line
}

// sampleIPs 在候选 IP 超过 SampleSize 时随机抽样。
// 用随机而非按序截取，是因为官方 IP 段按地区排列，
// 顺序截取会让结果永远集中在前几个段上。
func sampleIPs(ips []*net.IPAddr) []*net.IPAddr {
	if SampleSize <= 0 || len(ips) <= SampleSize {
		return ips
	}
	// 部分 Fisher-Yates：只洗出前 SampleSize 个即可
	for i := 0; i < SampleSize; i++ {
		j := i + rand.Intn(len(ips)-i)
		ips[i], ips[j] = ips[j], ips[i]
	}
	return ips[:SampleSize]
}
