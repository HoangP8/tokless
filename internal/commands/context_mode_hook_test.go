package commands

import (
	"testing"
)

func TestExtractNudge(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "echo with double quotes",
			input: `echo "hello world"`,
			want:  `hello world`,
		},
		{
			name:  "echo with single quotes",
			input: `echo 'single quoted'`,
			want:  `single quoted`,
		},
		{
			name:  "echo with escaped quotes",
			input: `echo "with \"escaped\" quotes"`,
			want:  `with "escaped" quotes`,
		},
		{
			name:  "leading and trailing spaces",
			input: `  echo "leading space"  `,
			want:  `leading space`,
		},
		{
			name:  "no echo prefix",
			input: `ls -la`,
			want:  `ls -la`,
		},
		{
			name:  "empty echo",
			input: `echo `,
			want:  `echo`,
		},
		{
			name:  "unterminated quote",
			input: `echo "unterminated`,
			want:  `"unterminated`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNudge(tt.input)
			if got != tt.want {
				t.Errorf("extractNudge(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
