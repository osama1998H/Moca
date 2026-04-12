package main

import "testing"

func TestDoctypeToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SalesOrder", "sales_order"},
		{"User", "user"},
		{"DocType", "doc_type"},
		{"simple", "simple"},
		{"", ""},
		{"Library Management", "library_management"},
		{"Sales Order", "sales_order"},
		{"my-doctype", "my_doctype"},
		{"Multi Word Name", "multi_word_name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := doctypeToSnake(tt.input)
			if got != tt.want {
				t.Errorf("doctypeToSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
