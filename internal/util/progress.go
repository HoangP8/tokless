package util

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Progress struct {
	title   string
	current string
	phase   string
	frac    float64
	start   time.Time
	frame   int
	active  bool
	mu      sync.Mutex
	stop    chan struct{}
	tty     bool
	out     *os.File
}


func NewProgress(title string) *Progress {
	return &Progress{title: title, tty: stdoutIsTTY(), out: os.Stdout}
}

func (p *Progress) Start(total int) {
	if p.title != "" {
		fmt.Fprintln(p.out, "\n  "+C.Bold(C.Cyan(p.title)))
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
			if p.active {
				p.frame = (p.frame + 1) % len(frames)
				p.repaint()
			}
			p.mu.Unlock()
		}
	}
}

// Begin starts a new item at 0% and resets its phase/elapsed.
func (p *Progress) Begin(label string) {
	p.mu.Lock()
	p.current = label
	p.phase = ""
	p.frac = 0
	p.start = time.Now()
	p.active = true
	if p.tty {
		p.repaint()
	}
	p.mu.Unlock()
}

// Step updates the active item's phase label and 0..1 fraction.
func (p *Progress) Step(phase string, frac float64) {
	p.mu.Lock()
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	p.phase = phase
	p.frac = frac
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

func fracBar(frac float64) string {
	width := 16
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return C.Green(strings.Repeat("█", filled)) + C.Gray(strings.Repeat("░", width-filled))
}

func elapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s <= 0 {
		return ""
	}
	return C.Gray(fmt.Sprintf(" %ds", s))
}

func (p *Progress) Complete(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.clearLine()
	noteStr := ""
	if note != "" {
		noteStr = C.Gray(" " + note)
	}
	fmt.Fprintf(p.out, "  %s %s %s%s\n", C.Green(Sym.Check), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] 100%%", fracBar(1))), noteStr)
}

func (p *Progress) Fail(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.clearLine()
	fmt.Fprintf(p.out, "  %s %s %s\n", C.Red(Sym.Cross), padEnd(p.current, 16), C.Red(reason))
}

func (p *Progress) Skip(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.clearLine()
	fmt.Fprintf(p.out, "  %s %s %s\n", C.Gray(Sym.Bullet), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] 100%%  %s", fracBar(1), note)))
}

func (p *Progress) Done(summary string) {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.mu.Lock()
	p.active = false
	p.clearLine()
	p.mu.Unlock()
	if summary != "" {
		fmt.Fprintln(p.out, "  "+C.Gray(summary))
	}
}

func (p *Progress) repaint() {
	if !p.tty || p.current == "" || !p.active {
		return
	}
	pct := int(p.frac*100 + 0.5)
	phase := ""
	if p.phase != "" {
		phase = C.Gray("  " + p.phase)
	}
	line := fmt.Sprintf("  %s %s %s%s%s", C.Cyan(frames[p.frame]), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] %3d%%", fracBar(p.frac), pct)), phase, elapsed(time.Since(p.start)))
	fmt.Fprint(p.out, "\r\x1b[2K"+line)
}

func (p *Progress) clearLine() {
	if p.tty {
		fmt.Fprint(p.out, "\r\x1b[2K")
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
