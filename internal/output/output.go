// Package output handles formatted output for dotpak.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
)

// Mode represents the output mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeQuiet
	ModeJSON
)

// Output handles formatted output with different modes.
type Output struct {
	mode      Mode
	verbose   bool
	writer    io.Writer
	errWriter io.Writer
}

// New creates a new Output with the specified mode.
func New(mode Mode, verbose bool) *Output {
	return &Output{
		mode:      mode,
		verbose:   verbose,
		writer:    os.Stdout,
		errWriter: os.Stderr,
	}
}

// SetWriter sets the output writer (for testing).
func (o *Output) SetWriter(w io.Writer) {
	o.writer = w
}

// SetErrWriter sets the error writer (for testing).
func (o *Output) SetErrWriter(w io.Writer) {
	o.errWriter = w
}

// Print outputs a message in normal mode.
func (o *Output) Print(format string, args ...any) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	fmt.Fprintf(o.writer, format, args...)
}

// Println outputs a message with newline in normal mode.
func (o *Output) Println(args ...any) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	fmt.Fprintln(o.writer, args...)
}

// Verbose outputs only when verbose mode is enabled.
func (o *Output) Verbose(format string, args ...any) {
	if !o.verbose || o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	fmt.Fprintf(o.writer, format, args...)
}

// Error outputs to stderr (always shown except in JSON mode).
func (o *Output) Error(format string, args ...any) {
	if o.mode == ModeJSON {
		return
	}
	color.New(color.FgRed).Fprintf(o.errWriter, "Error: "+format, args...)
}

// Warning outputs a warning message.
func (o *Output) Warning(format string, args ...any) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	color.New(color.FgYellow).Fprintf(o.writer, "Warning: "+format, args...)
}

// Success outputs a success message.
func (o *Output) Success(format string, args ...any) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	color.New(color.FgGreen).Fprintf(o.writer, format, args...)
}

// Info outputs an info message.
func (o *Output) Info(format string, args ...any) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	color.New(color.FgCyan).Fprintf(o.writer, format, args...)
}

// Progress outputs progress information.
func (o *Output) Progress(current, total int, item string) {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	fmt.Fprintf(o.writer, "\r[%d/%d] %s", current, total, truncate(item, 60))
}

// ClearProgress clears the progress line.
func (o *Output) ClearProgress() {
	if o.mode == ModeQuiet || o.mode == ModeJSON {
		return
	}
	fmt.Fprint(o.writer, "\r\033[K")
}

// JSON outputs data as JSON.
func (o *Output) JSON(data any) error {
	if o.mode != ModeJSON {
		return nil
	}
	encoder := json.NewEncoder(o.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// JSONCompact outputs data as compact JSON.
func (o *Output) JSONCompact(data any) error {
	if o.mode != ModeJSON {
		return nil
	}
	return json.NewEncoder(o.writer).Encode(data)
}

// DiffOutput handles diff-specific output with colors.
type DiffOutput struct {
	*Output
}

// NewDiffOutput creates a DiffOutput.
func NewDiffOutput(out *Output) *DiffOutput {
	return &DiffOutput{Output: out}
}

// Added outputs text in green (added lines).
func (d *DiffOutput) Added(text string) {
	if d.mode == ModeJSON || d.mode == ModeQuiet {
		return
	}
	color.New(color.FgGreen).Fprintln(d.writer, text)
}

// Removed outputs text in red (removed lines).
func (d *DiffOutput) Removed(text string) {
	if d.mode == ModeJSON || d.mode == ModeQuiet {
		return
	}
	color.New(color.FgRed).Fprintln(d.writer, text)
}

// Changed outputs text in yellow (change summary).
func (d *DiffOutput) Changed(text string) {
	if d.mode == ModeJSON || d.mode == ModeQuiet {
		return
	}
	color.New(color.FgYellow).Fprintln(d.writer, text)
}

// Header outputs diff header in cyan.
func (d *DiffOutput) Header(text string) {
	if d.mode == ModeJSON || d.mode == ModeQuiet {
		return
	}
	color.New(color.FgCyan).Fprintln(d.writer, text)
}

// truncate truncates a string to maxLen runes, preserving Unicode characters.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	return string(runes[:maxLen-3]) + "..."
}
