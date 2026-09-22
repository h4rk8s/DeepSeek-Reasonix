package cli

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// startupProgress keeps slow pre-TUI resume work visibly alive. Bubble Tea
// cannot render until the session is bound, so this owns one temporary terminal
// line and clears it before the program takes over the screen.
type startupProgress struct {
	out   io.Writer
	start time.Time
	stage string
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
}

func newStartupProgress(out io.Writer, enabled bool, stage string) *startupProgress {
	p := &startupProgress{out: out, start: time.Now(), stage: stage}
	if !enabled || out == nil {
		return p
	}
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	p.render(time.Now())
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				p.render(now)
			case <-p.stop:
				return
			}
		}
	}()
	return p
}

func (p *startupProgress) Set(stage string) {
	if p == nil || p.stop == nil {
		return
	}
	p.mu.Lock()
	p.stage = stage
	p.mu.Unlock()
	p.render(time.Now())
}

func (p *startupProgress) Stop() {
	if p == nil || p.stop == nil {
		return
	}
	p.once.Do(func() {
		close(p.stop)
		<-p.done
		p.mu.Lock()
		_, _ = fmt.Fprint(p.out, "\r\x1b[2K")
		p.mu.Unlock()
	})
}

func (p *startupProgress) render(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = fmt.Fprint(p.out, startupProgressLine(p.stage, now.Sub(p.start)))
}

func startupProgressLine(stage string, elapsed time.Duration) string {
	seconds := max(0, int(elapsed/time.Second))
	return fmt.Sprintf("\r\x1b[2K  ⋯ %s · %ds", stage, seconds)
}
