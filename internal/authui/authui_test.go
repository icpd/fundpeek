package authui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icpd/fundsync/internal/model"
)

func TestResendBlockedDuringCooldown(t *testing.T) {
	m := newModel(context.Background(), nil, model.SourceReal)
	m.step = stepCode
	m.cooldownUntil = time.Now().Add(time.Minute)

	next, cmd := m.handleResend()
	got := next.(authModel)
	if cmd != nil {
		t.Fatal("expected resend command to be blocked during cooldown")
	}
	if got.busy {
		t.Fatal("model should not become busy while resend is blocked")
	}
}

func TestQRRefreshBlockedDuringCooldown(t *testing.T) {
	m := newModel(context.Background(), nil, model.SourceYangJiBao)
	m.step = stepQR
	m.qrCooldown = time.Now().Add(time.Minute)

	next, cmd := m.handleResend()
	got := next.(authModel)
	if cmd != nil {
		t.Fatal("expected qr refresh command to be blocked during cooldown")
	}
	if got.busy {
		t.Fatal("model should not become busy while qr refresh is blocked")
	}
}

func TestContactInputAcceptsPhoneDigits(t *testing.T) {
	m := newModel(context.Background(), nil, model.SourceXiaoBei)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	got := next.(authModel)
	if got.input.Value() != "1" {
		t.Fatalf("input value = %q, want 1", got.input.Value())
	}
}

func TestContactInputAcceptsLetterR(t *testing.T) {
	m := newModel(context.Background(), nil, model.SourceReal)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	got := next.(authModel)
	if got.input.Value() != "r" {
		t.Fatalf("input value = %q, want r", got.input.Value())
	}
}

func TestQuitSetsCancelledError(t *testing.T) {
	m := newModel(context.Background(), nil, model.SourceYangJiBao)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	got := next.(authModel)
	if got.finalErr != ErrCancelled {
		t.Fatalf("finalErr = %v, want ErrCancelled", got.finalErr)
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}
