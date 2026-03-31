package output

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Mode determines the output format.
type Mode int

const (
	ModeTTY   Mode = iota // Default: human-readable with color
	ModeJSON              // --json: machine-readable JSON
	ModeTable             // --table: tabular output
)

// Writer provides formatted output for CLI commands.
// Commands obtain a Writer via NewWriter(cmd) and use its methods
// instead of writing directly to stdout.
type Writer struct {
	w       io.Writer
	errW    io.Writer
	color   *ColorConfig
	mode    Mode
	verbose bool
}

// NewWriter creates a Writer by reading output flags from the command.
// Must be called inside RunE (after Cobra has parsed flags).
func NewWriter(cmd *cobra.Command) *Writer {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	tableFlag, _ := cmd.Flags().GetBool("table")
	noColorFlag, _ := cmd.Flags().GetBool("no-color")
	verboseFlag, _ := cmd.Flags().GetBool("verbose")

	mode := ModeTTY
	if jsonFlag {
		mode = ModeJSON
	} else if tableFlag {
		mode = ModeTable
	}

	// JSON mode always disables color to avoid ANSI codes in output.
	out := cmd.OutOrStdout()
	cc := NewColorConfig(noColorFlag || mode == ModeJSON, out)

	return &Writer{
		w:       out,
		errW:    cmd.ErrOrStderr(),
		mode:    mode,
		color:   cc,
		verbose: verboseFlag,
	}
}

// NewWriterWithOptions creates a Writer with explicit settings. Used in tests.
func NewWriterWithOptions(w, errW io.Writer, mode Mode, color *ColorConfig, verbose bool) *Writer {
	return &Writer{
		w:       w,
		errW:    errW,
		mode:    mode,
		color:   color,
		verbose: verbose,
	}
}

// Mode returns the current output mode.
func (wr *Writer) Mode() Mode { return wr.mode }

// Color returns the ColorConfig for direct color access.
func (wr *Writer) Color() *ColorConfig { return wr.color }

// Verbose returns true when --verbose was set.
func (wr *Writer) Verbose() bool { return wr.verbose }

// Print writes a formatted line to stdout. No-op in JSON mode.
func (wr *Writer) Print(format string, args ...any) {
	if wr.mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(wr.w, format+"\n", args...)
}

// PrintJSON writes v as indented JSON. No-op in non-JSON mode.
func (wr *Writer) PrintJSON(v any) error {
	if wr.mode != ModeJSON {
		return nil
	}
	return WriteJSON(wr.w, v)
}

// PrintTable renders headers and rows as a formatted table.
// Works in TTY and Table modes. No-op in JSON mode.
func (wr *Writer) PrintTable(headers []string, rows [][]string) error {
	if wr.mode == ModeJSON {
		return nil
	}
	t := NewTable(headers, wr.color)
	for _, row := range rows {
		t.AddRow(row...)
	}
	return t.Render(wr.w)
}

// PrintSuccess prints a green success message.
func (wr *Writer) PrintSuccess(msg string) {
	if wr.mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(wr.w, "%s %s\n", wr.color.Success("✓"), msg)
}

// PrintWarning prints a yellow warning message.
func (wr *Writer) PrintWarning(msg string) {
	if wr.mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(wr.w, "%s %s\n", wr.color.Warning("!"), msg)
}

// PrintError prints a red error message to stderr.
func (wr *Writer) PrintError(msg string) {
	if wr.mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(wr.errW, "%s %s\n", wr.color.Error("✗"), msg)
}

// PrintInfo prints a cyan info message.
func (wr *Writer) PrintInfo(msg string) {
	if wr.mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(wr.w, "%s %s\n", wr.color.Info("ℹ"), msg)
}

// Debugf prints a muted debug message. Only outputs when --verbose is set.
func (wr *Writer) Debugf(format string, args ...any) {
	if !wr.verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(wr.errW, "%s\n", wr.color.Muted(msg))
}

// NewSpinner creates a Spinner tied to this Writer's output and color config.
func (wr *Writer) NewSpinner(message string) *Spinner {
	return NewSpinner(message, wr.w, wr.color)
}

// NewProgressBar creates a ProgressBar tied to this Writer's output and color config.
func (wr *Writer) NewProgressBar(total int) *ProgressBar {
	return NewProgressBar(total, wr.w, wr.color)
}
