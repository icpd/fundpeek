package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lewis/fundsync/internal/app"
	"github.com/lewis/fundsync/internal/authui"
	"github.com/lewis/fundsync/internal/config"
	"github.com/lewis/fundsync/internal/console"
	"github.com/lewis/fundsync/internal/credential"
	"github.com/lewis/fundsync/internal/model"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fundsync: %v\n", err)
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

	timeout := 2 * time.Minute
	if args[0] == "auth" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	case "auth", "status", "sync", "backup", "restore", "logout":
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
	fmt.Println(`fundsync

Usage:
  fundsync auth real|r
  fundsync auth yjb|yj
  fundsync auth xb|xbyj
  fundsync status
  fundsync sync yjb|yj
  fundsync sync xb|xbyj
  fundsync sync all|a
  fundsync backup
  fundsync restore <backup-file> [--yes]
  fundsync logout <real|r|yjb|yj|xb|xbyj>`)
}
