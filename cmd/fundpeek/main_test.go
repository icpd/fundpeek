package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAuthSourceAliases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "r", want: "real"},
		{input: "real", want: "real"},
		{input: "yjb", want: "yangjibao"},
		{input: "yj", want: "yangjibao"},
		{input: "yangjibao", want: "yangjibao"},
		{input: "xb", want: "xiaobei"},
		{input: "xbyj", want: "xiaobei"},
		{input: "xiaobei", want: "xiaobei"},
	}

	for _, tt := range tests {
		got, err := normalizeAuthSource(tt.input)
		if err != nil {
			t.Fatalf("normalizeAuthSource(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeAuthSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeAuthSourceRejectsAll(t *testing.T) {
	if _, err := normalizeAuthSource("a"); err == nil {
		t.Fatal("expected all alias to be rejected")
	}
}

func TestNormalizeSyncSourceAliases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "yjb", want: "yangjibao"},
		{input: "yj", want: "yangjibao"},
		{input: "yangjibao", want: "yangjibao"},
		{input: "xb", want: "xiaobei"},
		{input: "xbyj", want: "xiaobei"},
		{input: "xiaobei", want: "xiaobei"},
		{input: "a", want: "all"},
		{input: "all", want: "all"},
	}

	for _, tt := range tests {
		got, err := normalizeSyncSource(tt.input)
		if err != nil {
			t.Fatalf("normalizeSyncSource(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeSyncSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeSyncSourceRejectsReal(t *testing.T) {
	if _, err := normalizeSyncSource("r"); err == nil {
		t.Fatal("expected real alias to be rejected")
	}
}

func TestNormalizeSyncSourceDefaultsToAll(t *testing.T) {
	got, err := normalizeSyncSource("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "all" {
		t.Fatalf("normalizeSyncSource(\"\") = %q, want all", got)
	}
}

func TestHelpDoesNotCreateConfigFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUNDPEEK_CONFIG_DIR", dir)
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"fundpeek", "help"}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "device_id")); !os.IsNotExist(err) {
		t.Fatalf("help should not create device_id, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("help should not create backup dir, stat err: %v", err)
	}
}

func TestHelpIncludesCommandDescriptionsAndExamples(t *testing.T) {
	out := captureStdout(t, printUsage)

	for _, want := range []string{
		"fundpeek - 基金持仓 TUI 和可选基估宝同步工具",
		"Commands:",
		"auth <source>",
		"登录数据源",
		"tui",
		"打开基金估值和持仓 TUI",
		"json",
		"输出基金持仓和行情 JSON",
		"watch <action>",
		"管理自选股票",
		"stock <action>",
		"查询股票数据",
		"刷新本地持仓数据",
		"push real",
		"Sources:",
		"real",
		"yangjibao",
		"Examples:",
		"fundpeek sync",
		"fundpeek watch add 600519",
		"fundpeek stock quote 600519",
		"fundpeek json",
		"fundpeek push real",
		"fundpeek help sync",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"backup", "restore"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("help should not mention %q:\n%s", unwanted, out)
		}
	}
}

func TestSubcommandHelpIncludesUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "auth",
			args: []string{"auth", "--help"},
			want: []string{"fundpeek auth - 登录数据源", "Usage:", "fundpeek auth <source>", "yangjibao", "fundpeek auth yjb"},
		},
		{
			name: "status",
			args: []string{"status", "--help"},
			want: []string{"fundpeek status - 查看各数据源登录状态", "Usage:", "fundpeek status"},
		},
		{
			name: "tui",
			args: []string{"tui", "--help"},
			want: []string{"fundpeek tui - 打开基金估值和持仓 TUI", "Usage:", "fundpeek tui", "r 刷新当前页"},
		},
		{
			name: "json",
			args: []string{"json", "--help"},
			want: []string{"fundpeek json - 输出基金持仓和行情 JSON", "Usage:", "fundpeek json", "errors 字段"},
		},
		{
			name: "watch",
			args: []string{"watch", "--help"},
			want: []string{"fundpeek watch - 管理自选股票", "Usage:", "fundpeek watch add <code-or-name>", "watchlist.json"},
		},
		{
			name: "stock",
			args: []string{"stock", "--help"},
			want: []string{"fundpeek stock - 查询股票数据", "Usage:", "fundpeek stock quote <code>", "fundpeek stock list", "JSON"},
		},
		{
			name: "sync",
			args: []string{"sync", "--help"},
			want: []string{"fundpeek sync - 刷新本地持仓数据", "Usage:", "fundpeek sync [source]", "默认值"},
		},
		{
			name: "push",
			args: []string{"push", "--help"},
			want: []string{"fundpeek push - 推送本地持仓数据到远端", "Usage:", "fundpeek push real", "不会自动推送远端数据"},
		},
		{
			name: "push target trailing help",
			args: []string{"push", "real", "--help"},
			want: []string{"fundpeek push - 推送本地持仓数据到远端", "Usage:", "fundpeek push real"},
		},
		{
			name: "logout",
			args: []string{"logout", "--help"},
			want: []string{"fundpeek logout - 退出指定数据源登录", "Usage:", "fundpeek logout <source>", "fundpeek logout yjb"},
		},
		{
			name: "help topic",
			args: []string{"help", "sync"},
			want: []string{"fundpeek sync - 刷新本地持仓数据", "fundpeek sync [source]"},
		},
		{
			name: "help argument",
			args: []string{"sync", "help"},
			want: []string{"fundpeek sync - 刷新本地持仓数据", "fundpeek sync [source]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runWithStdout(t, tt.args...)
			if err != nil {
				t.Fatalf("run(%v) returned error: %v", tt.args, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Fatalf("subcommand help missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestSubcommandHelpDoesNotCreateConfigFiles(t *testing.T) {
	tests := [][]string{
		{"help", "sync"},
		{"sync", "--help"},
		{"sync", "help"},
		{"push", "real", "--help"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("FUNDPEEK_CONFIG_DIR", dir)

			if _, err := runWithStdout(t, args...); err != nil {
				t.Fatalf("run(%v) returned error: %v", args, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "device_id")); !os.IsNotExist(err) {
				t.Fatalf("subcommand help should not create device_id, stat err: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
				t.Fatalf("subcommand help should not create backup dir, stat err: %v", err)
			}
		})
	}
}

func TestUnknownHelpTopicDoesNotCreateConfigFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUNDPEEK_CONFIG_DIR", dir)

	out, err := runWithStdout(t, "help", "wat")
	if err == nil || !strings.Contains(err.Error(), "unknown help topic") {
		t.Fatalf("run(help wat) err = %v, want unknown help topic", err)
	}
	if !strings.Contains(out, "fundpeek <command> [arguments]") {
		t.Fatalf("unknown help topic should print top-level usage:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "device_id")); !os.IsNotExist(err) {
		t.Fatalf("unknown help topic should not create device_id, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("unknown help topic should not create backup dir, stat err: %v", err)
	}
}

func TestJSONIsKnownCommand(t *testing.T) {
	if !isKnownCommand("json") {
		t.Fatal("json should be a known command")
	}
	if !isKnownCommand("watch") {
		t.Fatal("watch should be a known command")
	}
	if !isKnownCommand("stock") {
		t.Fatal("stock should be a known command")
	}
}

func TestBackupAndRestoreAreUnknownCommands(t *testing.T) {
	for _, command := range []string{"backup", "restore"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("FUNDPEEK_CONFIG_DIR", dir)
			oldArgs := os.Args
			t.Cleanup(func() { os.Args = oldArgs })

			os.Args = []string{"fundpeek", command}
			err := run()
			if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("run(%q) err = %v, want unknown command", command, err)
			}
		})
	}
}

func TestUnknownCommandDoesNotCreateConfigFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUNDPEEK_CONFIG_DIR", dir)
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"fundpeek", "wat"}
	if err := run(); err == nil {
		t.Fatal("expected unknown command error")
	}
	if _, err := os.Stat(filepath.Join(dir, "device_id")); !os.IsNotExist(err) {
		t.Fatalf("unknown command should not create device_id, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("unknown command should not create backup dir, stat err: %v", err)
	}
}

func runWithStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = append([]string{"fundpeek"}, args...)
	var runErr error
	out := captureStdout(t, func() {
		runErr = run()
	})
	return out, runErr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
