package orm

import "testing"

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"", "''"},
		{"it's", "'it''s'"},
		{"O'Brien's", "'O''Brien''s'"},
		{"no quotes", "'no quotes'"},
		{"'already'", "'''already'''"},
	}
	for _, tt := range tests {
		got := QuoteLiteral(tt.input)
		if got != tt.want {
			t.Errorf("QuoteLiteral(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeGUCName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"company", "company"},
		{"user_company", "user_company"},
		{"Company", "company"},
		{"my-key", "mykey"},
		{"key with spaces", "keywithspaces"},
		{"key!@#$%", "key"},
		{"_leading", "_leading"},
		{"123numeric", "123numeric"},
		{"", ""},
	}
	for _, tt := range tests {
		got := SanitizeGUCName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeGUCName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
