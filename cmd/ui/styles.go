package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

var (
	stdout io.Writer = colorable.NewColorableStdout()
	stderr io.Writer = colorable.NewColorableStderr()
)

var (
	bold      = color.New(color.Bold)
	headerBar = color.New(color.FgCyan)
	headerTxt = color.New(color.Bold, color.FgCyan)
	successC  = color.New(color.Bold, color.FgGreen)
	warnC     = color.New(color.Bold, color.FgYellow)
	errC      = color.New(color.Bold, color.FgRed)
	infoC     = color.New(color.FgCyan)
	dimC      = color.New(color.Faint)
	subjectC  = color.New(color.Bold, color.FgWhite)
	breakC    = color.New(color.Bold, color.FgRed)
	hunkC     = color.New(color.FgYellow)
	fileC     = color.New(color.Faint, color.FgWhite)
	numC      = color.New(color.Faint, color.FgCyan)
)

var typeColors = map[string]*color.Color{
	"feat":     color.New(color.Bold, color.FgGreen),
	"fix":      color.New(color.Bold, color.FgRed),
	"refactor": color.New(color.Bold, color.FgYellow),
	"perf":     color.New(color.Bold, color.FgMagenta),
	"docs":     color.New(color.Bold, color.FgBlue),
	"test":     color.New(color.Bold, color.FgCyan),
	"chore":    color.New(color.Faint),
}

func typeColor(t string) *color.Color {
	if c, ok := typeColors[t]; ok {
		return c
	}
	return dimC
}

// Spinner shows an animated elapsed-time indicator during long operations.
type Spinner struct {
	msg  string
	stop chan struct{}
	done chan struct{}
}

func NewSpinner(msg string) *Spinner {
	return &Spinner{msg: msg, stop: make(chan struct{}), done: make(chan struct{})}
}

func (s *Spinner) Start() {
	go func() {
		defer close(s.done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		start := time.Now()
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				fmt.Fprintf(stderr, "\r%s\r", strings.Repeat(" ", 70))
				return
			case <-ticker.C:
				elapsed := time.Since(start).Truncate(time.Second)
				infoC.Fprintf(stderr, "\r  %s %s %s", frames[i%len(frames)], s.msg, elapsed)
				i++
			}
		}
	}()
}

func (s *Spinner) Stop() {
	close(s.stop)
	<-s.done
}

func Header(format string, a ...interface{})  { headerTxt.Fprintf(stderr, format, a...) }
func Success(format string, a ...interface{}) { successC.Fprintf(stdout, format, a...) }
func Warn(format string, a ...interface{})    { warnC.Fprintf(stderr, format, a...) }
func Error(format string, a ...interface{})   { errC.Fprintf(stderr, format, a...) }
func Info(format string, a ...interface{})    { infoC.Fprintf(stderr, format, a...) }
func Dim(format string, a ...interface{})     { dimC.Fprintf(stdout, format, a...) }

func PrintPlanSummary(toolVersion, baseRef string, commits []CommitDisplay) {
	fmt.Fprintln(stdout)
	headerBar.Fprintf(stdout, "  ┌─────────────────────────────────────────────────────┐\n")
	headerTxt.Fprintf(stdout, "  │ ")
	bold.Fprintf(stdout, "Commit Plan")
	dimC.Fprintf(stdout, "  %d commit(s)", len(commits))
	headerTxt.Fprintf(stdout, "\n")
	dimC.Fprintf(stdout, "  │ base: %.12s", baseRef)
	if toolVersion != "" {
		dimC.Fprintf(stdout, "  •  engine v%s", toolVersion)
	}
	fmt.Fprintln(stdout)
	headerBar.Fprintf(stdout, "  └─────────────────────────────────────────────────────┘\n")
	fmt.Fprintln(stdout)

	for i, c := range commits {
		numC.Fprintf(stdout, "  %d ", i+1)

		tc := typeColor(c.Type)
		if c.Scope != "" {
			tc.Fprintf(stdout, "%s(%s)", c.Type, c.Scope)
		} else {
			tc.Fprintf(stdout, "%s", c.Type)
		}
		dimC.Fprintf(stdout, ": ")
		subjectC.Fprintf(stdout, "%s\n", c.Subject)

		if c.Body != "" {
			lines := wrapText(c.Body, 70)
			for _, line := range lines {
				dimC.Fprintf(stdout, "    %s\n", line)
			}
		}

		hunkC.Fprintf(stdout, "    %d hunk(s)", c.HunkCount)
		if len(c.Files) > 0 {
			dimC.Fprintf(stdout, "  →  ")
			fileC.Fprintf(stdout, "%s", joinFiles(c.Files, 3))
		}
		fmt.Fprintln(stdout)

		if c.Breaking {
			breakC.Fprintf(stdout, "    ⚠  BREAKING CHANGE\n")
		}

		if i < len(commits)-1 {
			fmt.Fprintln(stdout)
		}
	}

	fmt.Fprintln(stdout)
	headerBar.Fprintf(stdout, "  ─────────────────────────────────────────────────────\n")
}

type CommitDisplay struct {
	Type      string
	Scope     string
	Subject   string
	Body      string
	HunkCount int
	Files     []string
	Breaking  bool
}

func wrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}
	var lines []string
	for len(text) > width {
		idx := strings.LastIndex(text[:width], " ")
		if idx <= 0 {
			idx = width
		}
		lines = append(lines, text[:idx])
		text = strings.TrimLeft(text[idx:], " ")
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return lines
}

func joinFiles(files []string, max int) string {
	if len(files) <= max {
		return join(files)
	}
	return join(files[:max]) + fmt.Sprintf(" (+%d more)", len(files)-max)
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
