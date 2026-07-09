package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/icpd/fundpeek/internal/app"
	"github.com/icpd/fundpeek/internal/authui"
	"github.com/icpd/fundpeek/internal/config"
	"github.com/icpd/fundpeek/internal/console"
	"github.com/icpd/fundpeek/internal/credential"
	"github.com/icpd/fundpeek/internal/jsonexport"
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/stockexport"
	"github.com/icpd/fundpeek/internal/tui"
	"github.com/icpd/fundpeek/internal/watchlist"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fundpeek: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return nil
	}
	if handled, err := handleHelp(args); handled {
		return err
	}
	if !isKnownCommand(args[0]) {
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := credential.NewFileStore(cfg.CredentialPath)
	if err != nil {
		return err
	}
	a := app.New(cfg, store)

	var ctx context.Context
	var cancel context.CancelFunc
	if args[0] == "tui" {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		timeout := 2 * time.Minute
		if args[0] == "auth" {
			timeout = 10 * time.Minute
		}
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	defer cancel()

	switch args[0] {
	case "auth":
		if len(args) < 2 {
			return errors.New("missing auth source: real/r, yjb/yj, xb/xbyj")
		}
		source, err := normalizeAuthSource(args[1])
		if err != nil {
			return err
		}
		return runAuth(ctx, a, source)
	case "status":
		return a.Status(ctx)
	case "tui":
		if !console.IsTerminal(os.Stdout) {
			return errors.New("tui requires an interactive terminal")
		}
		return tui.Run(ctx, a)
	case "json":
		return jsonexport.Write(ctx, a, os.Stdout)
	case "watch":
		return runWatch(ctx, a, args[1:])
	case "stock":
		return runStock(ctx, a, args[1:])
	case "sync":
		sourceArg := ""
		if len(args) >= 2 {
			sourceArg = args[1]
		}
		source, err := normalizeSyncSource(sourceArg)
		if err != nil {
			return err
		}
		return a.Sync(ctx, source)
	case "push":
		if len(args) < 2 {
			return errors.New("missing push target: real/r")
		}
		target, err := normalizePushTarget(args[1])
		if err != nil {
			return err
		}
		if target == model.SourceReal {
			return a.PushReal(ctx)
		}
		return fmt.Errorf("unknown push target %q", args[1])
	case "logout":
		if len(args) < 2 {
			return errors.New("missing logout source: real/r, yjb/yj, xb/xbyj")
		}
		source, err := normalizeAuthSource(args[1])
		if err != nil {
			return err
		}
		return a.Logout(source)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func isHelpCommand(command string) bool {
	switch command {
	case "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func handleHelp(args []string) (bool, error) {
	if isHelpCommand(args[0]) {
		if len(args) == 1 {
			printUsage()
			return true, nil
		}
		if len(args) > 2 {
			printUsage()
			return true, errors.New("too many help arguments")
		}
		if isHelpCommand(args[1]) {
			printUsage()
			return true, nil
		}
		if !printCommandUsage(args[1]) {
			printUsage()
			return true, fmt.Errorf("unknown help topic %q", args[1])
		}
		return true, nil
	}

	if isKnownCommand(args[0]) && hasHelpArgument(args[1:]) {
		printCommandUsage(args[0])
		return true, nil
	}

	return false, nil
}

func hasHelpArgument(args []string) bool {
	for _, arg := range args {
		if isHelpCommand(arg) {
			return true
		}
	}
	return false
}

func isKnownCommand(command string) bool {
	switch command {
	case "auth", "status", "sync", "push", "logout", "tui", "json", "watch", "stock":
		return true
	default:
		return false
	}
}

func normalizeAuthSource(source string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "real", "r":
		return model.SourceReal, nil
	case "yangjibao", "yjb", "yj":
		return model.SourceYangJiBao, nil
	case "xiaobei", "xb", "xbyj":
		return model.SourceXiaoBei, nil
	}
	return "", fmt.Errorf("unknown source %q", source)
}

func normalizeSyncSource(source string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "":
		return "all", nil
	case "yangjibao", "yjb", "yj":
		return model.SourceYangJiBao, nil
	case "xiaobei", "xb", "xbyj":
		return model.SourceXiaoBei, nil
	case "all", "a":
		return "all", nil
	}
	return "", fmt.Errorf("unknown sync source %q", source)
}

func normalizePushTarget(target string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "real", "r":
		return model.SourceReal, nil
	}
	return "", fmt.Errorf("unknown push target %q", target)
}

func runAuth(ctx context.Context, a *app.App, source string) error {
	if console.IsTerminal(os.Stdin) && console.IsTerminal(os.Stdout) {
		return authui.Run(ctx, a, source)
	}

	reader := bufio.NewReader(os.Stdin)
	switch source {
	case "real":
		email := prompt(reader, "real email: ")
		if err := a.AuthRealStart(ctx, email); err != nil {
			return err
		}
		token := prompt(reader, "email OTP token: ")
		return a.AuthRealVerify(ctx, email, token)
	case "yangjibao":
		return a.AuthYangJiBao(ctx)
	case "xiaobei":
		phone := prompt(reader, "phone: ")
		if err := a.AuthXiaoBeiStart(ctx, phone); err != nil {
			return err
		}
		code := prompt(reader, "sms code: ")
		return a.AuthXiaoBeiVerify(ctx, phone, code)
	default:
		return fmt.Errorf("unknown auth source %q", source)
	}
}

func runWatch(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("missing watch action: list, add, remove")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New("watch list does not accept arguments")
		}
		items, err := a.Watchlist()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("watchlist: empty")
			return nil
		}
		for _, item := range items {
			fmt.Printf("%s %s %s\n", item.Market, item.Code, item.Name)
		}
		return nil
	case "add":
		if len(args) < 2 {
			return errors.New("missing stock code or name")
		}
		query := strings.Join(args[1:], " ")
		candidates, err := a.SearchWatchlistCandidates(ctx, query)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return fmt.Errorf("no A-share stock matched %q", query)
		}
		if len(candidates) > 1 {
			printWatchCandidates(candidates)
			return fmt.Errorf("multiple stocks matched %q; use a stock code", query)
		}
		if _, err := a.AddWatchlistItem(candidates[0]); err != nil {
			return err
		}
		printWatchAdded(candidates[0])
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return errors.New("usage: fundpeek watch remove <code>")
		}
		_, removed, err := a.RemoveWatchlistItem(args[1])
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("stock %q is not in watchlist", args[1])
		}
		fmt.Println("watch removed:", args[1])
		return nil
	default:
		return fmt.Errorf("unknown watch action %q", args[0])
	}
}

func runStock(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("missing stock action: search, quote, minute, list")
	}
	switch args[0] {
	case "search":
		if len(args) < 2 {
			return errors.New("usage: fundpeek stock search <query>")
		}
		return stockexport.WriteSearch(ctx, strings.Join(args[1:], " "), os.Stdout)
	case "quote":
		if len(args) != 2 {
			return errors.New("usage: fundpeek stock quote <code>")
		}
		return stockexport.WriteQuote(ctx, a, args[1], os.Stdout)
	case "minute":
		if len(args) != 2 {
			return errors.New("usage: fundpeek stock minute <code>")
		}
		return stockexport.WriteMinute(ctx, a, args[1], os.Stdout)
	case "list":
		if len(args) != 1 {
			return errors.New("stock list does not accept arguments")
		}
		return stockexport.WriteList(ctx, a, os.Stdout)
	default:
		return fmt.Errorf("unknown stock action %q", args[0])
	}
}

func printWatchCandidates(items []watchlist.Item) {
	fmt.Println("matched stocks:")
	for _, item := range items {
		fmt.Printf("  %s %s %s\n", item.Market, item.Code, item.Name)
	}
}

func printWatchAdded(item watchlist.Item) {
	label := strings.TrimSpace(item.Name)
	if label == "" {
		label = item.Code
	}
	fmt.Printf("watch added: %s %s %s\n", item.Market, item.Code, label)
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func printUsage() {
	fmt.Println(`fundpeek - 基金持仓 TUI 和可选基估宝同步工具

Usage:
  fundpeek <command> [arguments]

Commands:
  auth <source>                 登录数据源，支持 real、yangjibao、xiaobei
  status                        查看各数据源登录状态
  tui                           打开基金估值和持仓 TUI
  json                          输出基金持仓和行情 JSON
  watch <action>                管理自选股票，支持 list、add、remove
  stock <action>                查询股票数据，输出 JSON
  sync [source]                 刷新本地持仓数据，可选 yjb、xb、all，默认 all
  push real                     将本地持仓数据同步到基估宝
  logout <source>               退出指定数据源登录
  help [command]                显示帮助信息或指定子命令帮助

Sources:
  real        aliases: r
  yangjibao   aliases: yjb, yj
  xiaobei     aliases: xb, xbyj
  all         aliases: a        仅用于 sync

Examples:
  fundpeek auth yjb
  fundpeek sync
  fundpeek tui
  fundpeek watch add 600519
  fundpeek stock quote 600519
  fundpeek json
  fundpeek push real
  fundpeek help sync`)
}

func printCommandUsage(command string) bool {
	switch command {
	case "auth":
		fmt.Println(`fundpeek auth - 登录数据源

Usage:
  fundpeek auth <source>

Sources:
  real        aliases: r        基估宝邮箱 OTP
  yangjibao   aliases: yjb, yj   养基宝扫码登录
  xiaobei     aliases: xb, xbyj  小倍养基短信验证码

Examples:
  fundpeek auth yjb
  fundpeek auth xb
  fundpeek auth real`)
		return true
	case "status":
		fmt.Println(`fundpeek status - 查看各数据源登录状态

Usage:
  fundpeek status

Examples:
  fundpeek status`)
		return true
	case "tui":
		fmt.Println(`fundpeek tui - 打开基金估值和持仓 TUI

Usage:
  fundpeek tui

Notes:
  读取本地 portfolio 快照；首次使用前先执行 fundpeek sync。
  在交互界面内按 Enter/右方向进入明细，Esc/左方向返回或退出，r 刷新当前页，R 强制刷新相关缓存。

Examples:
  fundpeek tui`)
		return true
	case "json":
		fmt.Println(`fundpeek json - 输出基金持仓和行情 JSON

Usage:
  fundpeek json

Notes:
  读取本地 portfolio 快照并刷新基金行情。
  单只基金行情失败时仍保留基金，并在 errors 字段记录失败原因。

Examples:
  fundpeek json`)
		return true
	case "watch":
		fmt.Println(`fundpeek watch - 管理自选股票

Usage:
  fundpeek watch list
  fundpeek watch add <code-or-name>
  fundpeek watch remove <code>

Notes:
  第一版优先支持 A 股。add 可输入代码或名称；名称匹配多只股票时会列出候选，请改用股票代码添加。
  自选股票只保存在本地 watchlist.json，不会写入 portfolio 或基估宝远端配置。

Examples:
  fundpeek watch list
  fundpeek watch add 600519
  fundpeek watch add 贵州茅台
  fundpeek watch remove 600519`)
		return true
	case "stock":
		fmt.Println(`fundpeek stock - 查询股票数据

Usage:
  fundpeek stock search <query>
  fundpeek stock quote <code>
  fundpeek stock minute <code>
  fundpeek stock list

Notes:
  输出稳定 JSON，适合脚本或大模型读取。
  quote 和 minute 第一版支持 A 股代码；search 用于按名称查找 A 股候选。
  list 读取本地 watchlist.json 并刷新自选股行情，不修改自选股列表。

Examples:
  fundpeek stock search 茅台
  fundpeek stock quote 600519
  fundpeek stock minute 600519
  fundpeek stock list`)
		return true
	case "sync":
		fmt.Println(`fundpeek sync - 刷新本地持仓数据

Usage:
  fundpeek sync [source]

Sources:
  yangjibao   aliases: yjb, yj
  xiaobei     aliases: xb, xbyj
  all         aliases: a        默认值，同步所有已登录基金来源

Examples:
  fundpeek sync
  fundpeek sync yjb
  fundpeek sync xb
  fundpeek sync all`)
		return true
	case "push":
		fmt.Println(`fundpeek push - 推送本地持仓数据到远端

Usage:
  fundpeek push real

Targets:
  real        aliases: r        基估宝

Notes:
  只会在显式执行 push real 时写入基估宝；sync、tui 和 json 不会自动推送远端数据。

Examples:
  fundpeek push real`)
		return true
	case "logout":
		fmt.Println(`fundpeek logout - 退出指定数据源登录

Usage:
  fundpeek logout <source>

Sources:
  real        aliases: r
  yangjibao   aliases: yjb, yj
  xiaobei     aliases: xb, xbyj

Examples:
  fundpeek logout yjb
  fundpeek logout xb
  fundpeek logout real`)
		return true
	default:
		return false
	}
}
