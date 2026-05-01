package console

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mdp/qrterminal/v3"
)

type Spinner struct {
	w       io.Writer
	frames  []string
	started time.Time
	index   int
	enabled bool
}

func IsTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func PrintQR(w io.Writer, content string) {
	qrterminal.GenerateWithConfig(content, qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     w,
		HalfBlocks: true,
		QuietZone:  1,
	})
}

func NewSpinner(w io.Writer, enabled bool) *Spinner {
	return &Spinner{
		w:       w,
		frames:  []string{"|", "/", "-", "\\"},
		started: time.Now(),
		enabled: enabled,
	}
}

func (s *Spinner) Update(message string) {
	elapsed := int(time.Since(s.started).Seconds())
	text := fmt.Sprintf("%s %s %ds", message, s.frames[s.index%len(s.frames)], elapsed)
	s.index++
	if s.enabled {
		fmt.Fprintf(s.w, "\r\033[K%s", text)
		return
	}
	fmt.Fprintln(s.w, text)
}

func (s *Spinner) Done(message string) {
	if s.enabled {
		fmt.Fprintf(s.w, "\r\033[K%s\n", message)
		return
	}
	fmt.Fprintln(s.w, message)
}

func (s *Spinner) Clear() {
	if s.enabled {
		fmt.Fprint(s.w, "\r\033[K")
	}
}
