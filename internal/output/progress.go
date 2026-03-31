package output

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Spinner displays a spinning animation for long-running operations.
// In non-TTY mode, it prints the message once without animation.
type Spinner struct {
	w       io.Writer
	color   *ColorConfig
	done    chan struct{}
	message string
	frames  []string
	mu      sync.Mutex
	active  bool
}

// NewSpinner creates a spinner with the given message.
func NewSpinner(message string, w io.Writer, cc *ColorConfig) *Spinner {
	return &Spinner{
		message: message,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		w:       w,
		color:   cc,
		done:    make(chan struct{}),
	}
}

// Start begins the spinner animation in a background goroutine.
// If the output is not a TTY, prints the message once and returns.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	if !s.color.Enabled() {
		_, _ = fmt.Fprintf(s.w, "%s...\n", s.message)
		return
	}

	go func() {
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				frame := s.color.Info(s.frames[i%len(s.frames)])
				_, _ = fmt.Fprintf(s.w, "\r%s %s", frame, s.message)
				i++
			}
		}
	}()
}

// Stop halts the spinner and prints a final message.
func (s *Spinner) Stop(finalMessage string) {
	s.mu.Lock()
	wasActive := s.active
	s.active = false
	s.mu.Unlock()

	if !wasActive {
		return
	}

	select {
	case <-s.done:
	default:
		close(s.done)
	}

	if s.color.Enabled() {
		_, _ = fmt.Fprintf(s.w, "\r\033[2K%s\n", finalMessage)
	} else if finalMessage != "" {
		_, _ = fmt.Fprintln(s.w, finalMessage)
	}
}

// ProgressBar displays a progress bar for operations with known total steps.
// In non-TTY mode, it prints progress at 0%, 25%, 50%, 75%, and 100%.
type ProgressBar struct {
	w             io.Writer
	color         *ColorConfig
	mu            sync.Mutex
	total         int
	current       int
	width         int
	lastMilestone int
}

// NewProgressBar creates a progress bar with the given total step count.
func NewProgressBar(total int, w io.Writer, cc *ColorConfig) *ProgressBar {
	return &ProgressBar{
		total:         total,
		width:         40,
		w:             w,
		color:         cc,
		lastMilestone: -1,
	}
}

// Update sets the current progress and redraws the bar.
func (p *ProgressBar) Update(current int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current = current
	if p.total <= 0 {
		return
	}

	pct := (current * 100) / p.total

	if p.color.Enabled() {
		filled := (p.width * pct) / 100
		empty := p.width - filled
		bar := strings.Repeat("=", filled)
		if filled < p.width {
			bar += ">"
			empty--
			if empty < 0 {
				empty = 0
			}
		}
		bar += strings.Repeat(" ", empty)
		_, _ = fmt.Fprintf(p.w, "\r[%s] %3d%%", bar, pct)
	} else {
		// Non-TTY: print only at milestone percentages.
		milestone := (pct / 25) * 25
		if milestone > p.lastMilestone {
			p.lastMilestone = milestone
			_, _ = fmt.Fprintf(p.w, "Progress: %d%%\n", pct)
		}
	}
}

// Finish sets progress to 100% and prints a final newline.
func (p *ProgressBar) Finish() {
	p.Update(p.total)
	if p.color.Enabled() {
		_, _ = fmt.Fprintln(p.w)
	}
}
