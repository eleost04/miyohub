package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/auth"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/processlock"
	"github.com/eleost04/miyohub/internal/store"
	"github.com/eleost04/miyohub/internal/tasks"
	qrcode "github.com/skip2/go-qrcode"
)

type cliOptions struct {
	command, dataDir, host, webDir, user, account, admin string
	port, timeout                                        int
	logTail                                              int
	logComponent                                         string
	bbsState                                             bool
	passwordStdin, noImage, json                         bool
	run                                                  tasks.RunOptions
}

type stringList []string

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	o := cliOptions{dataDir: getenv("MIYOHUB_DATA_DIR", "data"), host: getenv("MIYOHUB_HOST", "127.0.0.1"), webDir: getenv("MIYOHUB_WEB_DIR", "web/dist"), timeout: 120}
	root := flag.NewFlagSet("miyohub", flag.ContinueOnError)
	root.SetOutput(output)
	root.StringVar(&o.dataDir, "data-dir", o.dataDir, "数据目录（也可在子命令之后设置）")
	showVersion := false
	root.BoolVar(&showVersion, "version", false, "显示版本")
	root.BoolVar(&showVersion, "v", false, "显示版本")
	root.Usage = func() {
		fmt.Fprintln(output, "MiyoHub · 签到与米游币兑换\n用法: miyohub [--data-dir DIR] <命令> [选项]\n\n  init     初始化加密状态（不覆盖已有数据）\n  login    米游社 APP 扫码绑定账号\n  run      按配置执行一次签到；不执行兑换或推送\n  serve    启动 Web 控制台与调度器（默认命令）\n  show     查看脱敏配置摘要\n  logs     只读查看脱敏日志，可与服务同时使用\n  version  查看版本\n\n使用 miyohub <命令> --help 查看选项。")
	}
	if err := root.Parse(args); err != nil {
		return o, err
	}
	if showVersion {
		o.command = "version"
		return o, nil
	}
	remaining := root.Args()
	o.command = "serve"
	if len(remaining) > 0 {
		o.command, remaining = remaining[0], remaining[1:]
	}
	if o.command == "help" {
		root.Usage()
		return o, flag.ErrHelp
	}
	if o.command == "version" {
		if len(remaining) != 0 {
			return o, errors.New("version 不接受额外参数")
		}
		return o, nil
	}
	fs := flag.NewFlagSet("miyohub "+o.command, flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&o.dataDir, "data-dir", o.dataDir, "数据目录；除只读 logs 外，同一目录只允许一个进程使用")
	var games stringList
	switch o.command {
	case "init":
		fs.StringVar(&o.admin, "admin", "", "可选：初始化管理员用户名，须配合 --password-stdin")
		fs.BoolVar(&o.passwordStdin, "password-stdin", false, "从受信管道或文件的第一行读取管理员密码；不接受终端明文输入")
	case "serve":
		port, _ := strconv.Atoi(getenv("MIYOHUB_PORT", "5890"))
		fs.IntVar(&o.port, "port", port, "监听端口")
		fs.StringVar(&o.host, "host", o.host, "监听地址；局域网部署使用 0.0.0.0")
		fs.StringVar(&o.webDir, "web-dir", o.webDir, "已构建前端目录")
	case "login":
		fs.StringVar(&o.user, "user", "", "账号所属的站内用户名；多用户时必填")
		fs.StringVar(&o.account, "account", "main", "账号名称或已有账号 ID；重新绑定须确认是同一米游社账号")
		fs.IntVar(&o.timeout, "timeout", 120, "扫码等待秒数（1–120）")
		fs.BoolVar(&o.noImage, "no-image", false, "仅显示终端二维码，不生成临时 PNG")
	case "run":
		fs.StringVar(&o.user, "user", "", "只执行该站内用户的账号；留空执行全部启用账号")
		fs.StringVar(&o.account, "account", "", "只执行该账号名称或 ID；同名账号须配合 --user")
		fs.BoolVar(&o.run.GamesOnly, "games-only", false, "仅执行已启用的游戏签到与云游戏签到")
		fs.BoolVar(&o.run.BBSOnly, "bbs-only", false, "仅执行已启用的米游币任务")
		fs.Var(&games, "game", "筛选已启用的游戏 / 云游戏，可重复；不会开启未启用的任务")
		fs.BoolVar(&o.json, "json", false, "输出本次执行的脱敏 JSON 汇总")
	case "show":
		fs.BoolVar(&o.json, "json", false, "输出脱敏 JSON 摘要，不包含密钥、地址或日志")
	case "logs":
		fs.BoolVar(&o.bbsState, "bbs-state", false, "仅查询米游币任务状态和开关，不执行任务或续期")
		fs.StringVar(&o.account, "account", "", "查询指定账号 ID 或名称，多账号时必填")
		fs.BoolVar(&o.json, "json", false, "输出脱敏日志 JSON")
		fs.IntVar(&o.logTail, "tail", 100, "最近日志条数（1–500）")
		fs.StringVar(&o.logComponent, "component", "", "按类型筛选，如 bbs / games / exchange / push")
	default:
		return o, fmt.Errorf("未知命令 %q，使用 --help 查看帮助", o.command)
	}
	if err := fs.Parse(remaining); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, errors.New("存在多余的位置参数，请使用 --help 查看用法")
	}
	if strings.TrimSpace(o.dataDir) == "" {
		return o, errors.New("数据目录不能为空")
	}
	if o.command == "serve" && (o.port < 1 || o.port > 65535 || strings.TrimSpace(o.host) == "") {
		return o, errors.New("监听地址不能为空，端口应为 1–65535")
	}
	if o.command == "login" && (o.timeout < 1 || o.timeout > 120 || strings.TrimSpace(o.account) == "") {
		return o, errors.New("账号名不能为空，扫码等待时间应为 1–120 秒")
	}
	if o.command == "logs" && (o.logTail < 1 || o.logTail > 500) {
		return o, errors.New("日志条数应为 1–500")
	}
	if o.command == "init" && ((o.admin != "") != o.passwordStdin) {
		return o, errors.New("--admin 与 --password-stdin 须一起使用")
	}
	o.run.Games = games
	if err := o.run.Validate(); err != nil {
		return o, err
	}
	return o, nil
}

func execute(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	o, err := parseCLI(args, diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.command == "version" {
		_, err := fmt.Fprintln(output, "MiyoHub", version)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(o.dataDir, "state.json")
	if o.command != "init" && o.command != "serve" {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return errors.New("状态尚未初始化，请先执行 init 或 serve")
		} else if err != nil {
			return err
		}
	}
	// Log snapshots are strictly read-only. Atomic state replacement makes them
	// safe while serve holds its writer lock; never open a writable Store here.
	if o.command == "logs" {
		state, err := store.OpenReadOnly(path)
		if err != nil {
			return err
		}
		if o.bbsState {
			return inspectBBS(ctx, state, o, output)
		}
		return showLogs(state, o, output)
	}
	lock, err := processlock.Acquire(o.dataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	password := ""
	if o.command == "init" {
		if _, err := os.Stat(path); err == nil {
			if o.admin == "" {
				return errors.New("状态已存在，不会覆盖；请使用 serve 或 show")
			}
			existing, err := store.OpenReadOnly(path)
			if err != nil {
				return err
			}
			for _, user := range existing.ListUsers() {
				if user.Role == "admin" {
					return errors.New("已有管理员，不会重置数据或密码")
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if o.admin != "" {
			password, err = readAdminPassword(input)
			if err != nil {
				return err
			}
		}
	}
	openStore := store.New
	if o.command == "show" {
		openStore = store.OpenReadOnly
	}
	state, err := openStore(path)
	if err != nil {
		return err
	}
	switch o.command {
	case "init":
		if o.admin != "" {
			_, token, err := state.CreateAdmin(o.admin, password)
			if err != nil {
				return err
			}
			if err := state.DeleteSession(token); err != nil {
				return err
			}
		}
		fmt.Fprintf(output, "已初始化加密状态: %s\n请同时保管 state.json 与 state.json.key，勿提交仓库。\n", path)
		if !state.HasAdmin() {
			fmt.Fprintln(output, "执行 serve 后，在首次打开的页面创建管理员。")
		}
		return nil
	case "serve":
		return serve(ctx, state, o, output)
	case "show":
		return showSummary(state, o, output)
	case "login":
		return loginAccountCLI(ctx, state, auth.NewQRManager(state), o, output)
	case "run":
		return runOnce(ctx, state, o, output)
	}
	return nil
}

func readAdminPassword(input io.Reader) (string, error) {
	if file, ok := input.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("请通过受信管道或私有文件输入密码，或在 Web 页面初始化管理员")
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(input, 130))
	scanner.Buffer(make([]byte, 130), 130)
	if !scanner.Scan() {
		return "", errors.New("未能读取密码")
	}
	password := scanner.Text()
	if len(password) < 8 || len(password) > 128 {
		return "", errors.New("密码应为 8–128 字节")
	}
	return password, nil
}

func selectUser(s *store.Store, name string, single bool) (string, error) {
	active := []model.User{}
	for _, u := range s.ListUsers() {
		if name != "" && strings.EqualFold(u.Username, name) {
			if u.Status != "active" {
				return "", errors.New("所属用户尚未激活或已停用")
			}
			return u.ID, nil
		}
		if u.Status == "active" {
			active = append(active, u)
		}
	}
	if name != "" {
		return "", errors.New("所属用户不存在")
	}
	if !single {
		return "", nil
	}
	if len(active) == 1 {
		return active[0].ID, nil
	}
	return "", errors.New("请先创建站内用户；存在多个用户时使用 --user 指定所属用户")
}

func selectAccounts(s *store.Store, user, selector string) ([]string, error) {
	userID, err := selectUser(s, user, false)
	if err != nil {
		return nil, err
	}
	accounts := s.AccountsForUser(userID, userID == "")
	ids := []string{}
	if selector != "" {
		for _, a := range accounts {
			if a.ID == selector {
				return []string{a.ID}, nil
			}
		}
	}
	for _, a := range accounts {
		if selector != "" && a.ID != selector && a.Name != selector {
			continue
		}
		ids = append(ids, a.ID)
	}
	if selector != "" && len(ids) > 1 {
		return nil, errors.New("账号名不唯一，请使用 --user 或账号 ID 精确选择")
	}
	if len(ids) == 0 {
		return nil, errors.New("没有匹配的账号")
	}
	return ids, nil
}

type qrLogin interface {
	StartBinding(context.Context, string, string, string) (model.LoginState, error)
	State(string) model.LoginState
	Stop()
}

func loginAccountCLI(ctx context.Context, state *store.Store, manager qrLogin, o cliOptions, output io.Writer) error {
	defer manager.Stop()
	userID, err := selectUser(state, o.user, true)
	if err != nil {
		return err
	}
	accountID := ""
	for _, a := range state.AccountsForUser("", true) {
		if a.ID == o.account && a.UserID != userID {
			return errors.New("不能重新绑定其他用户的账号")
		}
	}
	for _, a := range state.AccountsForUser(userID, false) {
		if a.ID == o.account || a.Name == o.account {
			accountID = a.ID
			break
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.timeout)*time.Second)
	defer cancel()
	status, err := manager.StartBinding(ctx, userID, o.account, accountID)
	if err != nil {
		return err
	}
	qr, err := qrcode.New(status.QRURL, qrcode.Medium)
	if err != nil {
		return errors.New("二维码生成失败，请重试")
	}
	fmt.Fprintln(output, "请用米游社 APP 扫码并确认；临时二维码请勿分享或写入公开日志。")
	fmt.Fprintln(output, qr.ToSmallString(false))
	if !o.noImage {
		png, err := qr.PNG(320)
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(o.dataDir, ".login-qr-*.png")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(png)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(output, "临时二维码: %s（本次登录结束后自动清理）\n", file.Name())
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := manager.State(userID)
		if !status.Running {
			if status.Status == "success" {
				fmt.Fprintln(output, "账号已绑定，凭据已加密保存。执行 run 即可签到。")
				return nil
			}
			if status.Error != "" {
				return errors.New(status.Error)
			}
			return errors.New("扫码未完成，请重试")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("扫码登录已停止或超时: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type taskCounts struct {
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}
type runResult struct {
	AccountID string                `json:"account_id"`
	Name      string                `json:"name"`
	Updated   bool                  `json:"updated"`
	Results   map[string]taskCounts `json:"results"`
}

func runOnce(ctx context.Context, s *store.Store, o cliOptions, output io.Writer) error {
	ids, err := selectAccounts(s, o.user, o.account)
	if err != nil {
		return err
	}
	previous := map[string]time.Time{}
	for _, id := range ids {
		a, _ := s.AccountForUser("", true, id)
		previous[id] = a.LastTaskAt
	}
	runner := tasks.NewRunner(s)
	defer runner.Stop()
	if !o.json {
		fmt.Fprintf(output, "执行一次签到（%d 个所选账号）；不执行商品兑换或推送。\n", len(ids))
	}
	runErr := runner.RunWithOptions(ctx, ids, o.run)
	results := []runResult{}
	for _, id := range ids {
		a, _ := s.AccountForUser("", true, id)
		result := runResult{AccountID: id, Name: a.Name, Updated: !a.LastTaskAt.Equal(previous[id]), Results: map[string]taskCounts{}}
		if result.Updated {
			for key, summary := range a.TaskResults {
				result.Results[key] = taskCounts{summary.Success, summary.Failed, summary.Skipped}
			}
		}
		results = append(results, result)
		if !o.json {
			fmt.Fprintf(output, "账号 %q", a.Name)
			if !result.Updated {
				fmt.Fprintln(output, "：未执行 / 已停止")
				continue
			}
			fmt.Fprintln(output)
			for _, family := range []string{"games", "cloud", "bbs"} {
				if value, ok := result.Results[family]; ok {
					fmt.Fprintf(output, "  %s: 成功 %d / 失败 %d / 跳过 %d\n", family, value.Success, value.Failed, value.Skipped)
				}
			}
		}
	}
	if o.json {
		if err := json.NewEncoder(output).Encode(struct {
			OK       bool        `json:"ok"`
			Accounts []runResult `json:"accounts"`
		}{runErr == nil, results}); err != nil {
			return err
		}
	}
	return runErr
}

type accountSummary struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	UserID          string                     `json:"user_id"`
	Enabled         bool                       `json:"enabled"`
	HasCookie       bool                       `json:"has_cookie"`
	HasSToken       bool                       `json:"has_stoken"`
	CloudGames      []string                   `json:"cloud_games"`
	TaskSettings    *model.AccountTaskSettings `json:"task_settings"`
	ExchangeAllowed bool                       `json:"exchange_allowed"`
	CaptchaSource   string                     `json:"captcha_source"`
}

func showSummary(s *store.Store, o cliOptions, output io.Writer) error {
	cfg := s.Config()
	accounts := []accountSummary{}
	for _, a := range cfg.Accounts {
		cloud := []string{}
		for key, token := range a.CloudTokens {
			if token != "" {
				cloud = append(cloud, key)
			}
		}
		sort.Strings(cloud)
		_, runnable := s.AccountRunnable(a.ID)
		accounts = append(accounts, accountSummary{
			ID: a.ID, Name: a.Name, UserID: a.UserID, Enabled: runnable,
			HasCookie: a.Cookie != "", HasSToken: a.Stoken != "", CloudGames: cloud,
			TaskSettings: a.TaskSettings, ExchangeAllowed: s.AccountCanExchange(a.ID),
			CaptchaSource: s.CaptchaSettingsForUser(a.UserID).Source,
		})
	}
	summary := struct {
		Enabled       bool             `json:"enabled"`
		Accounts      []accountSummary `json:"accounts"`
		Schedule      model.Schedule   `json:"schedule"`
		ExchangePlans int              `json:"exchange_plans"`
		Push          string           `json:"push"`
	}{cfg.Enabled, accounts, cfg.Schedule, len(cfg.Shop.Plans), "per_user"}
	if o.json {
		return json.NewEncoder(output).Encode(summary)
	}
	fmt.Fprintf(output, "站点运行总开关: %t\n共享每日调度: %t %s %s，随机延后 0–%d 分钟\n兑换计划: %d\n推送: 按用户独立配置，请在 Web「消息推送」中管理；仅 serve 发送自动通知\n", cfg.Enabled, cfg.Schedule.Enabled, cfg.Schedule.Time, cfg.Schedule.Timezone, cfg.Schedule.JitterMins, len(cfg.Shop.Plans))
	for _, a := range accounts {
		fmt.Fprintf(output, "账号 %q [%s] 用户 %s · 启用 %t · Cookie %t · SToken %t · 云游戏凭据 %v\n", a.Name, a.ID, a.UserID, a.Enabled, a.HasCookie, a.HasSToken, a.CloudGames)
		if p := a.TaskSettings; p != nil {
			fmt.Fprintf(output, "  自动参与 %t · 游戏签到 %t (%s) · 云游戏签到 %t (%s) · 米游币 %t (社区 %v)\n", p.Automatic, p.Features.GameCheckin, strings.Join(p.Games.Enabled, ", "), p.Features.CloudGameCheckin, strings.Join(p.CloudGames.Enabled, ", "), p.Features.BBSTasks, p.BBS.Forums)
		}
		fmt.Fprintf(output, "  可兑换 %t · 打码来源 %s（实际调用仍检查授权与渠道开关）\n", a.ExchangeAllowed, a.CaptchaSource)
	}
	return nil
}
