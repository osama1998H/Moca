package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table formats columnar data using text/tabwriter.
type Table struct {
	color   *ColorConfig
	headers []string
	rows    [][]string
}

// NewTable creates a table with the given column headers.
func NewTable(headers []string, cc *ColorConfig) *Table {
	return &Table{
		headers: headers,
		color:   cc,
	}
}

// AddRow appends a row of values. Panics if the column count does not
// match the header count (a programming error, not a user error).
func (t *Table) AddRow(values ...string) {
	if len(values) != len(t.headers) {
		panic(fmt.Sprintf("output.Table: row has %d columns, expected %d", len(values), len(t.headers)))
	}
	t.rows = append(t.rows, values)
}

// Render writes the formatted table to w using text/tabwriter.
func (t *Table) Render(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header row (bold).
	boldHeaders := make([]string, len(t.headers))
	for i, h := range t.headers {
		boldHeaders[i] = t.color.Bold(h)
	}
	if _, err := fmt.Fprintln(tw, strings.Join(boldHeaders, "\t")); err != nil {
		return err
	}

	// Data rows.
	for _, row := range t.rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	return tw.Flush()
}
