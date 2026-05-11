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
		if len(args) < 2 {
			return errors.New("missing sync source: yjb/yj, xb/xbyj, all/a")
		}
		source, err := normalizeSyncSource(args[1])
		if err != nil {
			return err
		}
		return a.Sync(ctx, source)
	case "backup":
		path, err := a.Backup(ctx)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	case "restore":
		if len(args) < 2 {
			return errors.New("missing backup file")
		}
		if !hasYesFlag(args[2:]) {
			reader := bufio.NewReader(os.Stdin)
			if !confirm(reader, "restore will overwrite real cloud config. Continue? [y/N]: ") {
				return errors.New("restore cancelled")
			}
		}
		return a.Restore(ctx, args[1])
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
	case "auth", "status", "sync", "backup", "restore", "logout", "tui":
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
	case "yangjibao", "yjb", "yj":
		return model.SourceYangJiBao, nil
	case "xiaobei", "xb", "xbyj":
		return model.SourceXiaoBei, nil
	case "all", "a":
		return "all", nil
	}
	return "", fmt.Errorf("unknown sync source %q", source)
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

func confirm(reader *bufio.Reader, label string) bool {
	answer := strings.ToLower(prompt(reader, label))
	return answer == "y" || answer == "yes"
}

func hasYesFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Println(`fundpeek - 基金持仓同步、估值查看和备份恢复工具

Usage:
  fundpeek <command> [arguments]

Commands:
  auth <source>                 登录数据源，支持 real、yangjibao、xiaobei
  status                        查看各数据源登录状态
  tui                           打开基金估值和持仓 TUI
  sync <source>                 同步基金持仓到 real，可选 yjb、xb、all
  backup                        备份 real 云端配置，输出备份文件路径
  restore <backup-file> [--yes] 从备份文件恢复 real 云端配置
  logout <source>               退出指定数据源登录
  help                          显示帮助信息

Sources:
  real        aliases: r
  yangjibao   aliases: yjb, yj
  xiaobei     aliases: xb, xbyj
  all         aliases: a        仅用于 sync

Examples:
  fundpeek auth yjb
  fundpeek sync all
  fundpeek tui
  fundpeek backup
  fundpeek restore ./backup.json --yes`)
}
