// yx 是 Cloudflare 优选 IP 测速工具，同时提供命令行与 Web 图形界面。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/122440367/yx-tools/internal/app"
)

var version = "3.0.0"

const usage = `Cloudflare 优选 IP 测速工具 v%s

用法:
  yx                        启动图形界面（默认，自动打开浏览器）
  yx web [选项]             启动图形界面
  yx test [选项]            命令行测速
  yx proxy [选项]           优选反代：从 CSV 提取 IP:端口，可接着重测
  yx upload [选项]          上报已有结果
  yx cron [选项]            管理定时任务（Linux/macOS）

测速选项 (test):
  -colo   地区机场码，多个用逗号分隔，如 HKG,SIN；留空测全部
  -ipv6   使用 IPv6 段
  -n      测速数量（默认 10）
  -sl     下载速度下限 MB/s（默认 1）
  -tl     平均延迟上限 ms（默认 1000）
  -t      延迟测速线程数（默认 200，最大 1000）
  -port   测速端口（默认 443）
  -url    测速地址
  -f      自定义 IP 文件或 HTTP(S) 链接；支持 IP、CIDR、IP:端口
  -c      参与延迟测速的候选 IP 数量，从官方段里随机抽（默认 0 不限，约 6000 个）
  -all    穷举每个网段的全部 IP（很慢，会忽略 -c）
  -http   用真实 HTTP 请求测延迟（含 TLS 握手与服务端响应），比 TCP 握手准
  -nodl   只测延迟，跳过下载测速
  -dt     单个 IP 的下载测速时长上限，秒（默认 10）
  -mt     整轮测速的时长上限，秒（默认 0 不限）；到点就拿已测出的结果收工
  -o      结果输出文件（默认 result.csv）

上报选项 (test 末尾追加，或单独用 upload):
  -upload api|worker|github|telegram   上报方式
  -domain / -uuid      Worker 域名与 UUID
  -worker-url / -worker-token  优质 IP Worker 地址与 SPD_API_TOKEN
  -repo / -token       GitHub 仓库与 Token
  -path                GitHub 文件路径（默认 cloudflare_ips.txt）
  -bot-token / -chat-id Telegram Bot Token 与 Chat ID
  -limit               上报数量（默认 0，即全部）
  -clear               上报前清空 Worker 已有 IP

任务通知选项 (仅 test/upload，需显式启用):
  -notify feishu       发送飞书任务汇总（不发送 IP 明细）
  -feishu-app-id       飞书 App ID（成功通知后可保存）
  -feishu-app-secret   每次必填，永不保存；直接参数可能进入 Shell 历史/进程列表
  -feishu-receive-id   单个接收目标 ID（多人请使用群聊）
  -feishu-receive-id-type chat_id/open_id/union_id/user_id/email（默认 chat_id）

界面选项 (web):
  -listen  监听地址（默认 127.0.0.1:8080，远程访问用 0.0.0.0:8080）
  -no-open 不自动打开浏览器

定时任务 (cron):
  -list            列出已登记的任务
  -add "测速参数"   添加任务，配合 -at 指定时间
  -at "0 */6 * * *" cron 时间表达式（默认每 6 小时）
  -replace         添加前先清掉本程序已有的任务
  -remove          清除本程序登记的全部任务

示例:
  yx                                     打开图形界面
  yx web -listen 0.0.0.0:8080            在服务器上跑，浏览器远程访问
  yx test -colo HKG,SIN -n 20            测香港和新加坡，取 20 个
  yx test -n 10 -upload api -domain a.b -uuid xxx -clear
  yx proxy -take 20                      生成 ips_ports.txt
  yx proxy -i 别人的结果.csv -test -n 10   导入外部 CSV 并对这些反代 IP 测速
  yx proxy -test -colo HKG -http          只留回源到香港的反代 IP
  yx cron -add "test -n 10 -sl 2" -at "0 */6 * * *" -replace
`

func main() {
	if len(os.Args) < 2 {
		runWeb([]string{})
		return
	}
	switch os.Args[1] {
	case "web":
		runWeb(os.Args[2:])
	case "test":
		runTest(os.Args[2:])
	case "proxy":
		runProxy(os.Args[2:])
	case "upload":
		runUpload(os.Args[2:])
	case "cron":
		runCron(os.Args[2:])
	case "-v", "--version", "version":
		fmt.Printf("yx v%s\n", version)
	case "-h", "--help", "help":
		fmt.Printf(usage, version)
	default:
		// 兼容直接带测速参数的写法
		if strings.HasPrefix(os.Args[1], "-") {
			runTest(os.Args[1:])
			return
		}
		fmt.Printf("未知命令: %s\n\n", os.Args[1])
		fmt.Printf(usage, version)
		os.Exit(1)
	}
}

// ── 图形界面 ──────────────────────────────────────
func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:8080", "监听地址")
	noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
	_ = fs.Parse(args)

	srv := app.NewServer()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "监听失败: %v\n", err)
		os.Exit(1)
	}
	url := "http://" + displayAddr(ln.Addr().String())
	fmt.Printf("图形界面已启动: %s\n按 Ctrl+C 退出\n", url)
	fmt.Printf("结果与配置存放于: %s\n", app.DataDir())
	if !*noOpen {
		go openBrowser(url)
	}

	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("\n正在退出…")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "服务异常: %v\n", err)
		os.Exit(1)
	}
}

func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1:" + port
	}
	return addr
}

func openBrowser(url string) {
	time.Sleep(400 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ── 命令行测速 ────────────────────────────────────
type uploadFlags struct {
	mode        *string
	domain      *string
	uuid        *string
	repo        *string
	token       *string
	path        *string
	workerURL   *string
	workerToken *string
	botToken    *string
	chatID      *string
	limit       *int
	clear       *bool
}

type notificationFlags struct {
	mode          *string
	appID         *string
	appSecret     *string
	receiveID     *string
	receiveIDType *string
}

type uploadOutcome struct {
	Mode   string
	Status string
	Count  int
}

var (
	runSpeedTest = app.Run
	readCSV      = app.ReadCSV
	writeCSV     = app.WriteCSV
	notifyFeishu = app.NotifyFeishu
)

func bindUploadFlags(fs *flag.FlagSet) uploadFlags {
	return uploadFlags{
		mode:        fs.String("upload", "", "上报方式: api / worker / github / telegram"),
		domain:      fs.String("domain", "", "Worker 域名"),
		uuid:        fs.String("uuid", "", "UUID 或自定义路径"),
		repo:        fs.String("repo", "", "GitHub 仓库 owner/repo"),
		token:       fs.String("token", "", "GitHub Token"),
		path:        fs.String("path", "", "GitHub 文件路径"),
		workerURL:   fs.String("worker-url", "", "优质 IP Worker 地址"),
		workerToken: fs.String("worker-token", "", "Worker SPD_API_TOKEN"),
		botToken:    fs.String("bot-token", "", "Telegram Bot Token"),
		chatID:      fs.String("chat-id", "", "Telegram Chat ID"),
		limit:       fs.Int("limit", 0, "上报数量，0 表示全部"),
		clear:       fs.Bool("clear", false, "上报前清空 Worker 已有 IP"),
	}
}

func bindNotificationFlags(fs *flag.FlagSet) notificationFlags {
	return notificationFlags{
		mode:          fs.String("notify", "", "任务通知方式: feishu"),
		appID:         fs.String("feishu-app-id", "", "飞书应用 App ID（可在通知成功后保存）"),
		appSecret:     fs.String("feishu-app-secret", "", "飞书应用 App Secret（必填且永不保存）"),
		receiveID:     fs.String("feishu-receive-id", "", "飞书单个接收目标 ID"),
		receiveIDType: fs.String("feishu-receive-id-type", "", "接收目标类型: chat_id/open_id/union_id/user_id/email"),
	}
}

func resolveNotification(nf notificationFlags, cfg *app.Config) (bool, app.FeishuTarget, error) {
	mode := strings.ToLower(strings.TrimSpace(*nf.mode))
	if mode == "" || mode == "none" {
		return false, app.FeishuTarget{}, nil
	}
	if mode != "feishu" {
		return false, app.FeishuTarget{}, fmt.Errorf("未知通知方式: %s", mode)
	}
	target := app.FeishuTarget{
		AppID:         strings.TrimSpace(*nf.appID),
		AppSecret:     strings.TrimSpace(*nf.appSecret),
		ReceiveID:     strings.TrimSpace(*nf.receiveID),
		ReceiveIDType: strings.TrimSpace(*nf.receiveIDType),
	}
	if target.AppID == "" {
		target.AppID = cfg.FeishuAppID
	}
	if target.ReceiveID == "" {
		target.ReceiveID = cfg.FeishuReceiveID
	}
	if target.ReceiveIDType == "" {
		target.ReceiveIDType = cfg.FeishuReceiveType
	}
	if target.ReceiveIDType == "" {
		target.ReceiveIDType = "chat_id"
	}
	if err := app.ValidateFeishuTarget(target); err != nil {
		return false, app.FeishuTarget{}, err
	}
	return true, target, nil
}

func runTest(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := executeTest(ctx, args); err != nil {
		fmt.Fprintf(os.Stderr, "任务失败: %v\n", err)
		os.Exit(1)
	}
}

func executeTest(ctx context.Context, args []string) error {
	started := time.Now()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	colo := fs.String("colo", "", "地区机场码，逗号分隔")
	ipv6 := fs.Bool("ipv6", false, "使用 IPv6")
	count := fs.Int("n", 10, "测速数量")
	speed := fs.Float64("sl", 1, "下载速度下限 MB/s")
	delay := fs.Int("tl", 1000, "平均延迟上限 ms")
	threads := fs.Int("t", 200, "延迟测速线程数")
	port := fs.Int("port", 443, "测速端口")
	url := fs.String("url", "", "测速地址")
	ipFile := fs.String("f", "", "自定义 IP 文件或 HTTP(S) 链接")
	sample := fs.Int("c", 0, "候选 IP 数量，0 表示不限")
	testAll := fs.Bool("all", false, "穷举全部 IP")
	httping := fs.Bool("http", false, "用真实 HTTP 请求测延迟")
	noDL := fs.Bool("nodl", false, "只测延迟")
	dlTimeout := fs.Int("dt", 10, "单个 IP 的下载测速时长上限（秒）")
	maxRun := fs.Int("mt", 0, "整轮测速的时长上限（秒），0 不限")
	out := fs.String("o", app.ResultFile, "结果输出文件")
	uf := bindUploadFlags(fs)
	nf := bindNotificationFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := app.LoadConfig()
	notifyEnabled, notifyTarget, err := resolveNotification(nf, cfg)
	if err != nil {
		return err
	}

	o := app.Options{
		Colo: *colo, IPv6: *ipv6, Count: *count,
		SpeedLimit: *speed, DelayLimit: *delay, Threads: *threads,
		Port: *port, TestURL: *url, IPFile: *ipFile, DisableDL: *noDL,
		SampleSize: *sample, TestAll: *testAll, HTTPing: *httping,
		DLTimeout: *dlTimeout, MaxRunTime: *maxRun,
		Verbose: true,
	}

	rs, testErr := runSpeedTest(ctx, o, reportProgress)
	testStatus, writeStatus := "success", "skipped"
	var primaryErr error
	if testErr != nil {
		testStatus = statusForError(testErr)
		primaryErr = fmt.Errorf("测速失败: %w", testErr)
	} else if len(rs) == 0 {
		testStatus = "failed"
		primaryErr = errors.New("测速结束但没有有效结果")
	} else {
		printResults(rs)
		if err := writeCSV(*out, rs); err != nil {
			writeStatus = "failed"
			primaryErr = errors.Join(primaryErr, fmt.Errorf("写入 %s 失败: %w", *out, err))
		} else {
			writeStatus = "success"
			fmt.Printf("\n结果已保存: %s\n", app.DataPath(*out))
		}
	}

	upload := uploadOutcome{Status: "skipped"}
	if testErr == nil && len(rs) > 0 {
		var uploadErr error
		upload, uploadErr = doUpload(ctx, uf, rs)
		if uploadErr != nil {
			primaryErr = errors.Join(primaryErr, uploadErr)
		}
	}
	ended := time.Now()
	totalStatus := "success"
	if primaryErr != nil {
		totalStatus = statusForError(primaryErr)
	}
	summary := app.TaskSummary{
		Operation: "test", Status: totalStatus, StartedAt: started, EndedAt: ended,
		ResultCount: len(rs), TestStatus: testStatus, WriteStatus: writeStatus, UploadMode: upload.Mode,
		UploadStatus: upload.Status, UploadCount: upload.Count,
	}
	_ = writeStatus // retained as a distinct lifecycle state even though the message contract omits it
	if primaryErr != nil {
		summary.Failure = primaryErr.Error()
	}
	if notifyEnabled {
		if notifyErr := finalizeNotification(ctx, cfg, notifyTarget, summary, uploadSecrets(uf)...); notifyErr != nil {
			primaryErr = errors.Join(primaryErr, notifyErr)
		}
	}
	return primaryErr
}

// reportProgress 打印测速过程。下载阶段逐条回来，测一个报一个，
// 不用等整批跑完才看到东西。
func reportProgress(p app.Progress) {
	if p.Result != nil {
		r := p.Result
		fmt.Printf("  %-18s %-6d %6.2f ms  %7.2f MB/s  %s\n",
			r.IP, r.Port, r.Delay, r.Speed, r.ColoName)
		return
	}
	fmt.Println("· " + p.Message)
}

func printResults(rs []app.Result) {
	if len(rs) == 0 {
		fmt.Println("没有符合条件的结果")
		return
	}
	fmt.Printf("\n%-18s %-6s %-9s %-11s %-7s %s\n", "IP", "端口", "延迟(ms)", "速度(MB/s)", "丢包", "地区")
	fmt.Println(strings.Repeat("-", 68))
	for _, r := range rs {
		fmt.Printf("%-18s %-6d %-9.2f %-11.2f %-7.0f%% %s\n",
			r.IP, r.Port, r.Delay, r.Speed, r.LossRate*100, r.ColoName)
	}
}

func doUpload(ctx context.Context, uf uploadFlags, rs []app.Result) (uploadOutcome, error) {
	mode := strings.ToLower(strings.TrimSpace(*uf.mode))
	if mode == "" || mode == "none" {
		return uploadOutcome{Status: "skipped"}, nil
	}
	outcome := uploadOutcome{Mode: mode, Status: "failed"}
	cfg := app.LoadConfig()
	switch mode {
	case "api":
		d, u := *uf.domain, *uf.uuid
		if d == "" {
			d = cfg.WorkerDomain
		}
		if u == "" {
			u = cfg.UUID
		}
		n, err := app.UploadToAPI(ctx, app.APITarget{Domain: d, UUID: u}, rs, *uf.limit, *uf.clear)
		if err != nil {
			return outcome, fmt.Errorf("上报到 Worker 失败: %w", err)
		}
		cfg.WorkerDomain, cfg.UUID = d, u
		if err := app.SaveConfig(cfg); err != nil {
			return outcome, fmt.Errorf("保存 Worker 配置: %w", err)
		}
		fmt.Printf("已上报 %d 个 IP 到 Worker\n", n)
		outcome.Status, outcome.Count = "success", n
	case "worker":
		workerURL, workerToken := *uf.workerURL, *uf.workerToken
		if workerURL == "" {
			workerURL = cfg.FastIPWorkerURL
		}
		if workerToken == "" {
			workerToken = cfg.FastIPWorkerToken
		}
		n, err := app.UploadToWorker(ctx, app.WorkerTarget{URL: workerURL, Token: workerToken}, rs, *uf.limit)
		if err != nil {
			return outcome, fmt.Errorf("上传到优质 IP Worker 失败: %w", err)
		}
		cfg.FastIPWorkerURL, cfg.FastIPWorkerToken = workerURL, workerToken
		if err := app.SaveConfig(cfg); err != nil {
			return outcome, fmt.Errorf("保存优质 IP Worker 配置: %w", err)
		}
		fmt.Printf("已上传 %d 个 IP 到优质 IP Worker\n", n)
		outcome.Status, outcome.Count = "success", n
	case "github":
		repo, token, path := *uf.repo, *uf.token, *uf.path
		if repo == "" {
			repo = cfg.GitHubRepo
		}
		if token == "" {
			token = cfg.GitHubToken
		}
		if path == "" {
			path = cfg.GitHubPath
		}
		n, err := app.UploadToGitHub(ctx, app.GitHubTarget{Repo: repo, Token: token, Path: path}, rs, *uf.limit)
		if err != nil {
			return outcome, fmt.Errorf("上传到 GitHub 失败: %w", err)
		}
		cfg.GitHubRepo, cfg.GitHubToken, cfg.GitHubPath = repo, token, path
		if err := app.SaveConfig(cfg); err != nil {
			return outcome, fmt.Errorf("保存 GitHub 配置: %w", err)
		}
		fmt.Printf("已上传 %d 个 IP 到 GitHub\n", n)
		outcome.Status, outcome.Count = "success", n
	case "telegram":
		botToken, chatID := *uf.botToken, *uf.chatID
		if botToken == "" {
			botToken = cfg.TelegramBotToken
		}
		if chatID == "" {
			chatID = cfg.TelegramChatID
		}
		n, err := app.UploadToTelegram(ctx, app.TelegramTarget{BotToken: botToken, ChatID: chatID}, rs, *uf.limit)
		if err != nil {
			return outcome, fmt.Errorf("发送到 Telegram 失败: %w", err)
		}
		cfg.TelegramBotToken, cfg.TelegramChatID = botToken, chatID
		if err := app.SaveConfig(cfg); err != nil {
			return outcome, fmt.Errorf("保存 Telegram 配置: %w", err)
		}
		fmt.Printf("已发送 %d 个 IP 到 Telegram\n", n)
		outcome.Status, outcome.Count = "success", n
	default:
		return outcome, fmt.Errorf("未知上报方式: %s", mode)
	}
	return outcome, nil
}

func statusForError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "failed"
}

func uploadSecrets(uf uploadFlags) []string {
	return []string{*uf.uuid, *uf.token, *uf.workerToken, *uf.botToken}
}

func finalizeNotification(parent context.Context, cfg *app.Config, target app.FeishuTarget, summary app.TaskSummary, secrets ...string) error {
	notifyCtx := parent
	var cancel context.CancelFunc
	if errors.Is(parent.Err(), context.Canceled) {
		notifyCtx, cancel = context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer cancel()
	}
	if err := notifyFeishu(notifyCtx, target, summary, secrets...); err != nil {
		return fmt.Errorf("飞书通知失败（主任务状态: %s）: %w", summary.Status, err)
	}
	cfg.FeishuAppID = target.AppID
	cfg.FeishuReceiveID = target.ReceiveID
	cfg.FeishuReceiveType = target.ReceiveIDType
	if err := app.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存飞书非秘密目标配置: %w", err)
	}
	return nil
}

// ── 优选反代 ──────────────────────────────────────
// 从任意测速 CSV 提取 IP:端口 生成列表，可接着拿这份列表重测一遍。
func runProxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	in := fs.String("i", app.ResultFile, "来源 CSV，可以是别人分享的结果")
	out := fs.String("o", app.ProxyListFile, "输出文件")
	take := fs.Int("take", 0, "从 CSV 取前 N 条，0 表示全部")
	test := fs.Bool("test", false, "生成后直接对这份列表测速")
	count := fs.Int("n", 10, "测速数量")
	speed := fs.Float64("sl", 0, "下载速度下限 MB/s")
	delay := fs.Int("tl", 1000, "平均延迟上限 ms")
	threads := fs.Int("t", 200, "延迟测速线程数")
	noDL := fs.Bool("nodl", false, "只测延迟")
	colo := fs.String("colo", "", "地区机场码，逗号分隔")
	httping := fs.Bool("http", false, "用真实 HTTP 请求测延迟")
	dlTimeout := fs.Int("dt", 10, "单个 IP 的下载测速时长上限（秒）")
	maxRun := fs.Int("mt", 0, "整轮测速的时长上限（秒），0 不限")
	uf := bindUploadFlags(fs)
	_ = fs.Parse(args)

	n, err := app.ProxyListFromCSV(*in, *out, *take)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成失败: %v\n", err)
		os.Exit(1)
	}
	outPath := app.DataPath(*out)
	fmt.Printf("已生成 %s，共 %d 条\n", outPath, n)
	if !*test {
		return
	}

	fmt.Println("开始对反代列表测速")
	o := app.Options{
		Proxy: true, IPFile: outPath, Count: *count,
		SpeedLimit: *speed, DelayLimit: *delay, Threads: *threads,
		Colo: *colo, HTTPing: *httping, DisableDL: *noDL, Verbose: true,
		DLTimeout: *dlTimeout, MaxRunTime: *maxRun,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rs, err := app.Run(ctx, o, reportProgress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "测速失败: %v\n", err)
		os.Exit(1)
	}
	printResults(rs)
	if err := app.WriteCSV(app.ResultFile, rs); err != nil {
		fmt.Fprintf(os.Stderr, "结果写入失败: %v\n", err)
	} else {
		fmt.Printf("\n结果已保存: %s\n", app.DataPath(app.ResultFile))
	}
	if _, err := doUpload(ctx, uf, rs); err != nil {
		fmt.Fprintf(os.Stderr, "上传失败: %v\n", err)
		os.Exit(1)
	}
}

// ── 上报已有结果 ──────────────────────────────────
func runUpload(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := executeUpload(ctx, args); err != nil {
		fmt.Fprintf(os.Stderr, "任务失败: %v\n", err)
		os.Exit(1)
	}
}

func executeUpload(ctx context.Context, args []string) error {
	started := time.Now()
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	in := fs.String("i", app.ResultFile, "测速结果 CSV")
	uf := bindUploadFlags(fs)
	nf := bindNotificationFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := app.LoadConfig()
	notifyEnabled, notifyTarget, err := resolveNotification(nf, cfg)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*uf.mode) == "" {
		return errors.New("请用 -upload api、worker、github 或 telegram 指定上报方式")
	}
	rs, err := readCSV(*in)
	if err != nil {
		return fmt.Errorf("读取失败: %w", err)
	}
	upload, primaryErr := doUpload(ctx, uf, rs)
	ended := time.Now()
	summary := app.TaskSummary{Operation: "upload", Status: "success", StartedAt: started, EndedAt: ended,
		ResultCount: len(rs), UploadMode: upload.Mode, UploadStatus: upload.Status, UploadCount: upload.Count}
	if primaryErr != nil {
		summary.Status = statusForError(primaryErr)
		summary.Failure = primaryErr.Error()
	}
	if notifyEnabled {
		if notifyErr := finalizeNotification(ctx, cfg, notifyTarget, summary, uploadSecrets(uf)...); notifyErr != nil {
			primaryErr = errors.Join(primaryErr, notifyErr)
		}
	}
	return primaryErr
}

// ── 定时任务 ──────────────────────────────────────
func runCron(args []string) {
	fs := flag.NewFlagSet("cron", flag.ExitOnError)
	list := fs.Bool("list", false, "列出已登记的任务")
	add := fs.String("add", "", `添加任务，值为测速参数，如 "test -n 10 -sl 2"`)
	at := fs.String("at", "0 */6 * * *", "cron 时间表达式")
	replace := fs.Bool("replace", false, "添加前先清掉已有任务")
	remove := fs.Bool("remove", false, "清除本程序登记的全部任务")
	_ = fs.Parse(args)

	if !app.CronSupported() {
		fmt.Fprintln(os.Stderr, "当前系统不支持 crontab。Windows 请用「任务计划程序」调用本程序。")
		os.Exit(1)
	}

	switch {
	case *remove:
		n, err := app.RemoveCronJobs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "清除失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已清除 %d 条定时任务\n", n)

	case *add != "":
		// 用绝对路径，并切到数据目录，保证结果文件落在能写的位置
		self := app.SelfPath()
		dir := app.DataDir()
		cmd := fmt.Sprintf("cd %s && %s %s >> yx-cron.log 2>&1", quote(dir), quote(self), *add)
		if err := app.AddCronJob(*at, cmd, *replace); err != nil {
			fmt.Fprintf(os.Stderr, "添加失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已添加定时任务\n  时间: %s\n  命令: %s\n", *at, cmd)
		fmt.Printf("日志会写入 %s\n", filepath.Join(dir, "yx-cron.log"))

	default:
		jobs, err := app.ListCronJobs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取失败: %v\n", err)
			os.Exit(1)
		}
		if len(jobs) == 0 {
			fmt.Println("还没有登记定时任务")
			fmt.Println(`添加示例: yx cron -add "test -n 10 -sl 2" -at "0 */6 * * *"`)
			return
		}
		fmt.Printf("共 %d 条定时任务:\n", len(jobs))
		for i, j := range jobs {
			fmt.Printf("  %d. [%s] %s\n", i+1, j.Schedule, j.Command)
		}
		_ = list
	}
}

// quote 给路径加引号，避免空格截断
func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
