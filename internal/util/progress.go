package util

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var frames = spinnerFrames()

func spinnerFrames() []string {
	if glyphsEnabled {
		return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	return []string{"|", "/", "-", "\\"}
}

type Progress struct {
	title      string
	current    string
	phase      string
	frac       float64
	start      time.Time
	frame      int
	active     bool
	mu         sync.Mutex
	stop       chan struct{}
	tty        bool
	out        *os.File
	treeStyle  bool
	treeRoot   bool
	rows       int
	lastNonTTY string
	lastWidth  int
}

func NewProgress(title string) *Progress {
	return &Progress{title: title, tty: stdoutIsTTY() && vtReady && !runningInsideClaudeCode(), out: os.Stdout}
}

// TreeStem is the vertical continuation prefix for nested installer rows.
func TreeStem() string { return C.Dim(pick("│   ", "|   ")) }

func treeCornerGlyph(root bool) string {
	if root {
		return pick("┌─ ", "+- ")
	}
	return pick("├─ ", "+- ")
}

// TreeTop opens the first branch (down-only connector).
func TreeTop(title string) {
	fmt.Println(C.Dim(treeCornerGlyph(true)) + C.Bold(title))
}

// TreeCorner opens a mid-tree branch under an active trunk.
func TreeCorner(title string) {
	fmt.Println(C.Dim(treeCornerGlyph(false)) + C.Bold(title))
}

// TreeCornerStyled opens a mid-tree branch; title already styled.
func TreeCornerStyled(title string) {
	fmt.Println(C.Dim(treeCornerGlyph(false)) + title)
}

// TreeLeaf prints one row under TreeStem().
func TreeLeaf(line string) {
	fmt.Println(TreeStem() + line)
}

// TreeClose ends the current trunk segment (connector only; trunk continues below).
func TreeClose() {
	fmt.Println(C.Dim(pick("│", "|")))
}

// TreeFooter closes the install tree with a rule line as the final branch.
func TreeFooter(ruleWidth int) {
	fmt.Println(C.Dim(pick("└", "+")) + C.Gray(Rule(ruleWidth)))
}

func NewSectionProgress(section string) *Progress {
	return &Progress{
		title:     section,
		tty:       stdoutIsTTY() && vtReady && !runningInsideClaudeCode(),
		out:       os.Stdout,
		treeStyle: true,
	}
}

// NewRootSectionProgress is the first tree section (┌─ heading).
func NewRootSectionProgress(section string) *Progress {
	p := NewSectionProgress(section)
	p.treeRoot = true
	return p
}

func (p *Progress) Start(total int) {
	if p.title != "" {
		if p.treeStyle {
			if p.tty {
				fmt.Fprintf(p.out, "%s %s\n", C.Magenta(C.Bold(pick("●", "*"))), C.Magenta(C.Bold(p.title)))
			} else if p.treeRoot {
				TreeTop(p.title)
			} else {
				TreeCorner(p.title)
			}
		} else {
			fmt.Fprintln(p.out, "\n  "+C.Bold(C.Cyan(p.title)))
		}
	}
	p.rows = 0
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
	p.lastNonTTY = ""
	p.start = time.Now()
	p.active = true
	if p.tty {
		p.repaint()
	}
	p.mu.Unlock()
}

func (p *Progress) indent() string {
	if p.treeStyle {
		return C.Dim(pick("│   ", "|   "))
	}
	return "  "
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
		p.mu.Unlock()
		return
	}
	if phase == "" || phase == p.lastNonTTY {
		p.mu.Unlock()
		return
	}
	p.lastNonTTY = phase
	label := p.current
	if label == "" {
		label = p.title
	}
	fmt.Fprintf(p.out, "  %s %s\n", C.Dim(pick("·", ".")), label+" — "+phase)
	p.mu.Unlock()
}

// VisibleLen counts the display width of s, skipping ANSI escape sequences.
func VisibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i++; i < len(s); i++ {
				c := s[i]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
					break
				}
			}
			continue
		}
		n++
	}
	return n
}


func EraseStyledLine(s string) string {
	_ = s
	if !stdoutIsTTY() || runningInsideClaudeCode() {
		return ""
	}
	return "\r\x1b[2K"
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
	fmt.Fprintf(p.out, "%s%s %s %s%s\n", p.indent(), C.Green(Sym.Check), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] 100%%", fracBar(1))), noteStr)
	if p.treeStyle {
		p.rows++
	}
}

func (p *Progress) Fail(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.clearLine()
	fmt.Fprintf(p.out, "%s%s %s %s\n", p.indent(), C.Red(Sym.Cross), padEnd(p.current, 16), C.Red(reason))
	if p.treeStyle {
		p.rows++
	}
}

func (p *Progress) Skip(note string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.clearLine()
	fmt.Fprintf(p.out, "%s%s %s %s\n", p.indent(), C.Gray(Sym.Bullet), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] 100%%  %s", fracBar(1), note)))
	if p.treeStyle {
		p.rows++
	}
}

func (p *Progress) Done(summary string) {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.mu.Lock()
	p.active = false
	p.clearLine()
	rows := p.rows
	title := p.title
	p.mu.Unlock()
	if p.treeStyle {
		if p.tty && title != "" {
			titleLine := C.Dim(treeCornerGlyph(p.treeRoot)) + C.Bold(title)
			up := rows + 1
			fmt.Fprintf(p.out, "\x1b[%dA\r\x1b[2K%s\x1b[%dB\r", up, titleLine, up)
		}
		TreeClose()
		return
	}
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
	line := fmt.Sprintf("%s%s %s %s%s%s", p.indent(), C.Cyan(frames[p.frame]), padEnd(p.current, 16),
		C.Gray(fmt.Sprintf("[%s] %3d%%", fracBar(p.frac), pct)), phase, elapsed(time.Since(p.start)))
	curWidth := VisibleLen(line)
	pad := 0
	if p.lastWidth > curWidth {
		pad = p.lastWidth - curWidth
	}
	p.lastWidth = curWidth
	fmt.Fprint(p.out, "\r\x1b[2K"+line+strings.Repeat(" ", pad))
}

func (p *Progress) clearLine() {
	if p.tty {
		p.lastWidth = 0
		fmt.Fprint(p.out, "\r\x1b[2K")
	}
}

// WithSilencedLogs redirects stdout/stderr to a buffer while fn runs.
func WithSilencedLogs(fn func() error) error {
	_, err := CaptureLogs(fn)
	return err
}

func CaptureLogs(fn func() error) (string, error) {
	realOut, realErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return "", fn()
	}
	os.Stdout, os.Stderr = w, w
	var captured strings.Builder
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				captured.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		close(done)
	}()
	var ferr error
	func() {
		defer func() {
			w.Close()
			os.Stdout, os.Stderr = realOut, realErr
			<-done
			r.Close()
		}()
		ferr = fn()
	}()
	return captured.String(), ferr
}
