package util

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Progress renders a single-line spinner + step list.
type Progress struct {
	title   string
	idx     int
	total   int
	current string
	frame   int
	mu      sync.Mutex
	stop    chan struct{}
	tty     bool
}

func NewProgress(title string) *Progress {
	return &Progress{title: title, tty: stdoutIsTTY()}
}

func (p *Progress) Start(total int) {
	p.total = total
	p.idx = 0
	if p.title != "" {
		fmt.Println("\n  " + C.Bold(C.Cyan(p.title)))
	}
	if p.tty {
		p.stop = make(chan struct{})
		go p.spin()
	}
}

func (p *Progress) spin() {
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			p.frame = (p.frame + 1) % len(frames)
			p.repaint()
			p.mu.Unlock()
		}
	}
}

func (p *Progress) Begin(label string) {
	p.mu.Lock()
	p.current = label
	if p.tty {
		p.repaint()
	}
	p.mu.Unlock()
}

func padEnd(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func (p *Progress) pctBar(pct int) string {
	width := 16
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	return C.Green(strings.Repeat("█", filled)) + C.Gray(strings.Repeat("░", width-filled))
}

func (p *Progress) Complete(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idx++
	p.clearLine()
	pct := 100
	if p.total > 0 {
		pct = p.idx * 100 / p.total
	}
	noteStr := ""
	if note != "" {
		noteStr = C.Gray(" " + note)
	}
	fmt.Printf("  %s %s %s%s\n", C.Green(Sym.Check), padEnd(p.current, 22),
		C.Gray(fmt.Sprintf("[%s] %d%%", p.pctBar(pct), pct)), noteStr)
}

func (p *Progress) Fail(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idx++
	p.clearLine()
	fmt.Printf("  %s %s %s\n", C.Red(Sym.Cross), padEnd(p.current, 22), C.Red(reason))
}

func (p *Progress) Skip(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idx++
	p.clearLine()
	pct := 100
	if p.total > 0 {
		pct = p.idx * 100 / p.total
	}
	fmt.Printf("  %s %s %s\n", C.Gray(Sym.Bullet), padEnd(p.current, 22),
		C.Gray(fmt.Sprintf("[%s] %d%%  %s", p.pctBar(pct), pct, note)))
}

func (p *Progress) Done(summary string) {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.mu.Lock()
	p.clearLine()
	p.mu.Unlock()
	if summary != "" {
		fmt.Println("  " + C.Gray(summary))
	}
}

func (p *Progress) repaint() {
	if !p.tty || p.current == "" {
		return
	}
	pct := 0
	if p.total > 0 {
		pct = p.idx * 100 / p.total
	}
	line := fmt.Sprintf("  %s %s %s", C.Cyan(frames[p.frame]), padEnd(p.current, 22),
		C.Gray(fmt.Sprintf("[%s] %d%%", p.pctBar(pct), pct)))
	fmt.Fprint(os.Stdout, "\r\x1b[2K"+line)
}

func (p *Progress) clearLine() {
	if p.tty {
		fmt.Fprint(os.Stdout, "\r\x1b[2K")
	}
}

// WithSilencedLogs redirects stdout/stderr to a buffer while fn runs.
func WithSilencedLogs(fn func() error) error {
	realOut, realErr := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr = w, w
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		close(done)
	}()
	defer func() {
		w.Close()
		os.Stdout, os.Stderr = realOut, realErr
		<-done
		r.Close()
	}()
	return fn()
}
