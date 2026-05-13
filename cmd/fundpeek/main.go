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
	"github.com/icpd/fundpeek/internal/model"
	"github.com/icpd/fundpeek/internal/tui"
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
	if isHelpCommand(args[0]) {
		printUsage()
		return nil
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

func isKnownCommand(command string) bool {
	switch command {
	case "auth", "status", "sync", "push", "logout", "tui":
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

func prompt(reader *bufio.Reader, label string) string {
	fmt.Print(label)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func printUsage() {
	fmt.Println(`fundpeek - 基金持仓 TUI 和可选估基宝同步工具

Usage:
  fundpeek <command> [arguments]

Commands:
  auth <source>                 登录数据源，支持 real、yangjibao、xiaobei
  status                        查看各数据源登录状态
  tui                           打开基金估值和持仓 TUI
  sync [source]                 刷新本地 TUI 持仓数据，可选 yjb、xb、all，默认 all
  push real                     将本地持仓数据同步到估基宝
  logout <source>               退出指定数据源登录
  help                          显示帮助信息

Sources:
  real        aliases: r
  yangjibao   aliases: yjb, yj
  xiaobei     aliases: xb, xbyj
  all         aliases: a        仅用于 sync

Examples:
  fundpeek auth yjb
  fundpeek sync
  fundpeek tui
  fundpeek push real`)
}
