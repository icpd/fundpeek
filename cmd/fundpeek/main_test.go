package main

import (
	"os"
	"path/filepath"
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
