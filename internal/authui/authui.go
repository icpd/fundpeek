package authui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lewis/fundsync/internal/app"
	"github.com/lewis/fundsync/internal/console"
	"github.com/lewis/fundsync/internal/model"
	"github.com/lewis/fundsync/internal/sources/yangjibao"
)

const (
	otpCooldown = time.Minute
	qrCooldown  = 30 * time.Second
	qrTTL       = time.Minute
	qrPollEvery = 2 * time.Second
)

var ErrCancelled = errors.New("auth cancelled")

type step int

const (
	stepContact step = iota
	stepCode
	stepQR
	stepDone
)

type authModel struct {
	ctx context.Context
	app *app.App

	source string
	step   step

	input     textinput.Model
	codeInput textinput.Model

	busy       bool
	polling    bool
	status     string
	errText    string
	finalErr   error
	successMsg string

	cooldownUntil time.Time
	qrCooldown    time.Time
	qrExpires     time.Time
	nextPoll      time.Time
	qrID          string
	qrView        string
}

type tickMsg time.Time

type sentMsg struct {
	err error
}

type verifiedMsg struct {
	err error
}

type qrLoadedMsg struct {
	id  string
	url string
	err error
}

type qrStateMsg struct {
	state yangjibao.QRCodeState
	err   error
}

type savedMsg struct {
	err error
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func Run(ctx context.Context, a *app.App, source string) error {
	m := newModel(ctx, a, source)
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return err
	}
	if m, ok := final.(authModel); ok {
		if m.finalErr != nil {
			return m.finalErr
		}
		if m.successMsg != "" {
			fmt.Println(m.successMsg)
		}
	}
	return nil
}

func newModel(ctx context.Context, a *app.App, source string) authModel {
	input := textinput.New()
	input.Width = 34
	input.Prompt = "> "

	codeInput := textinput.New()
	codeInput.Width = 18
	codeInput.Prompt = "> "

	m := authModel{
		ctx:       ctx,
		app:       a,
		source:    source,
		input:     input,
		codeInput: codeInput,
	}
	switch source {
	case model.SourceReal:
		m.input.Placeholder = "real email"
		m.input.Focus()
		m.status = "输入 real 邮箱，Enter 发送验证码"
	case model.SourceXiaoBei:
		m.input.Placeholder = "phone"
		m.input.Focus()
		m.status = "输入小倍养基手机号，Enter 发送短信验证码"
	case model.SourceYangJiBao:
		m.step = stepQR
		m.status = "正在生成二维码"
	default:
		m.finalErr = fmt.Errorf("unknown auth source %q", source)
	}
	return m
}

func (m authModel) Init() tea.Cmd {
	if m.finalErr != nil {
		return tea.Quit
	}
	if m.source == model.SourceYangJiBao {
		return tea.Batch(tickCmd(), m.loadQR())
	}
	return tickCmd()
}

func (m authModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.finalErr = ErrCancelled
			return m, tea.Quit
		case "q":
			if m.step == stepQR {
				m.finalErr = ErrCancelled
				return m, tea.Quit
			}
		case "enter":
			return m.handleEnter()
		case "r":
			if m.step == stepCode || m.step == stepQR {
				return m.handleResend()
			}
		}
	case tickMsg:
		cmds = append(cmds, tickCmd())
		if m.source == model.SourceYangJiBao {
			now := time.Time(msg)
			if !m.qrExpires.IsZero() && now.After(m.qrExpires) {
				m.status = "二维码已超时，按 r 刷新"
			}
			if m.canPollQR(now) {
				m.polling = true
				m.nextPoll = now.Add(qrPollEvery)
				cmds = append(cmds, m.pollQR())
			}
		}
	case sentMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "发送失败，可检查输入后重试"
			break
		}
		m.errText = ""
		m.cooldownUntil = time.Now().Add(otpCooldown)
		m.step = stepCode
		m.input.Blur()
		cmds = append(cmds, m.codeInput.Focus())
		m.status = "验证码已发送，输入验证码后按 Enter"
	case verifiedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "验证失败，可修改验证码后重试"
			break
		}
		m.step = stepDone
		m.successMsg = successMessage(m.source)
		return m, tea.Quit
	case qrLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "二维码生成失败，冷却结束后按 r 重试"
			m.qrCooldown = time.Now().Add(qrCooldown)
			break
		}
		m.errText = ""
		m.qrID = msg.id
		m.qrView = renderQR(msg.url)
		now := time.Now()
		m.qrExpires = now.Add(qrTTL)
		m.qrCooldown = now.Add(qrCooldown)
		m.nextPoll = now
		m.status = "请使用养基宝扫码"
	case qrStateMsg:
		m.polling = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "轮询失败，会继续等待；也可以按 r 刷新二维码"
			break
		}
		switch msg.state.State {
		case yangjibao.StateConfirmed:
			if msg.state.Token == "" {
				m.errText = "yangjibao confirmed but token is empty"
				break
			}
			m.busy = true
			m.status = "扫码成功，正在保存授权"
			return m, m.saveYangJiBao(msg.state.Token)
		case yangjibao.StateExpired:
			m.status = "二维码已超时，按 r 刷新"
			m.qrExpires = time.Now().Add(-time.Second)
		default:
			m.status = "等待扫码"
		}
	case savedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "授权保存失败"
			break
		}
		m.step = stepDone
		m.successMsg = successMessage(m.source)
		return m, tea.Quit
	}

	if m.step == stepContact && !m.busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.step == stepCode && !m.busy {
		var cmd tea.Cmd
		m.codeInput, cmd = m.codeInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m authModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title(m.source)))
	b.WriteString("\n\n")

	if m.status != "" {
		b.WriteString(labelStyle.Render("状态: "))
		if m.step == stepDone {
			b.WriteString(okStyle.Render(m.status))
		} else {
			b.WriteString(m.status)
		}
		b.WriteString("\n")
	}
	if m.errText != "" {
		b.WriteString(errStyle.Render("错误: " + m.errText))
		b.WriteString("\n")
	}
	if m.busy {
		b.WriteString(labelStyle.Render("处理中..."))
		b.WriteString("\n")
	}

	switch m.step {
	case stepContact:
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(contactLabel(m.source)))
		b.WriteString("\n")
		b.WriteString(m.input.View())
	case stepCode:
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(contactLabel(m.source)))
		b.WriteString(" ")
		b.WriteString(m.input.Value())
		b.WriteString("\n\n")
		b.WriteString(labelStyle.Render("验证码"))
		b.WriteString("\n")
		b.WriteString(m.codeInput.View())
		b.WriteString("\n\n")
		b.WriteString(cooldownLine("重新发送", m.cooldownUntil))
	case stepQR:
		if m.qrView != "" {
			b.WriteString("\n")
			b.WriteString(m.qrView)
		}
		b.WriteString("\n")
		b.WriteString(cooldownLine("刷新二维码", m.qrCooldown))
		if !m.qrExpires.IsZero() {
			b.WriteString("  ")
			b.WriteString(countdownLine("二维码过期", m.qrExpires))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(helpText(m.step)))
	return b.String()
}

func (m authModel) handleEnter() (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch m.step {
	case stepContact:
		m.busy = true
		m.status = "正在发送验证码"
		m.errText = ""
		return m, m.sendCode()
	case stepCode:
		m.busy = true
		m.status = "正在验证"
		m.errText = ""
		return m, m.verifyCode()
	}
	return m, nil
}

func (m authModel) handleResend() (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	now := time.Now()
	switch m.step {
	case stepCode:
		if now.Before(m.cooldownUntil) {
			return m, nil
		}
		m.busy = true
		m.status = "正在重新发送验证码"
		m.errText = ""
		return m, m.sendCode()
	case stepQR:
		if now.Before(m.qrCooldown) {
			return m, nil
		}
		m.busy = true
		m.status = "正在刷新二维码"
		m.errText = ""
		return m, m.loadQR()
	}
	return m, nil
}

func (m authModel) canPollQR(now time.Time) bool {
	if m.qrID == "" || m.polling || m.busy {
		return false
	}
	if !m.qrExpires.IsZero() && now.After(m.qrExpires) {
		return false
	}
	return m.nextPoll.IsZero() || !now.Before(m.nextPoll)
}

func (m authModel) sendCode() tea.Cmd {
	source := m.source
	value := m.input.Value()
	return func() tea.Msg {
		var err error
		switch source {
		case model.SourceReal:
			err = m.app.SendRealOTP(m.ctx, value)
		case model.SourceXiaoBei:
			err = m.app.SendXiaoBeiSMS(m.ctx, value)
		default:
			err = fmt.Errorf("unsupported code auth source %q", source)
		}
		return sentMsg{err: err}
	}
}

func (m authModel) verifyCode() tea.Cmd {
	source := m.source
	contact := m.input.Value()
	code := m.codeInput.Value()
	return func() tea.Msg {
		var err error
		switch source {
		case model.SourceReal:
			err = m.app.VerifyRealOTP(m.ctx, contact, code)
		case model.SourceXiaoBei:
			err = m.app.VerifyXiaoBeiSMS(m.ctx, contact, code)
		default:
			err = fmt.Errorf("unsupported code auth source %q", source)
		}
		return verifiedMsg{err: err}
	}
}

func (m authModel) loadQR() tea.Cmd {
	return func() tea.Msg {
		qr, err := m.app.NewYangJiBaoQRCode(m.ctx)
		return qrLoadedMsg{id: qr.QRID, url: qr.QRURL, err: err}
	}
}

func (m authModel) pollQR() tea.Cmd {
	id := m.qrID
	return func() tea.Msg {
		state, err := m.app.CheckYangJiBaoQRCode(m.ctx, id)
		return qrStateMsg{state: state, err: err}
	}
}

func (m authModel) saveYangJiBao(token string) tea.Cmd {
	return func() tea.Msg {
		return savedMsg{err: m.app.SaveYangJiBaoToken(token)}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func renderQR(content string) string {
	var buf bytes.Buffer
	console.PrintQR(&buf, content)
	return buf.String()
}

func title(source string) string {
	switch source {
	case model.SourceReal:
		return "real 授权"
	case model.SourceYangJiBao:
		return "养基宝授权"
	case model.SourceXiaoBei:
		return "小倍养基授权"
	default:
		return "授权"
	}
}

func contactLabel(source string) string {
	switch source {
	case model.SourceReal:
		return "邮箱"
	case model.SourceXiaoBei:
		return "手机号"
	default:
		return "账号"
	}
}

func successMessage(source string) string {
	switch source {
	case model.SourceReal:
		return "real authenticated"
	case model.SourceYangJiBao:
		return "养基宝已授权"
	case model.SourceXiaoBei:
		return "xiaobei authenticated"
	default:
		return "authenticated"
	}
}

func helpText(step step) string {
	if step == stepQR {
		return "r 刷新二维码  q/Esc 退出"
	}
	if step == stepCode {
		return "Enter 确认  r 重新发送  Esc 退出"
	}
	return "Enter 确认  Esc 退出"
}

func cooldownLine(action string, until time.Time) string {
	if until.IsZero() {
		return fmt.Sprintf("%s: 可用", action)
	}
	remaining := remainingSeconds(until)
	if remaining <= 0 {
		return fmt.Sprintf("%s: 可用", action)
	}
	return fmt.Sprintf("%s: %ds", action, remaining)
}

func countdownLine(label string, until time.Time) string {
	remaining := remainingSeconds(until)
	if remaining <= 0 {
		return fmt.Sprintf("%s: 已过期", label)
	}
	return fmt.Sprintf("%s: %ds", label, remaining)
}

func remainingSeconds(until time.Time) int {
	return int(math.Ceil(time.Until(until).Seconds()))
}
