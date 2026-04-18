package runner

import "testing"

func TestParseRecoveryHints(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []RecoveryHint
	}{
		{
			name: "none",
			in:   "nothing to see here",
			want: nil,
		},
		{
			name: "tier larger",
			in:   "run stopped\nneed-tier: larger\n",
			want: []RecoveryHint{{"tier", "larger"}},
		},
		{
			name: "backend cheaper plus tier",
			in:   "need-backend: cheaper\nneed-tier: larger\n",
			want: []RecoveryHint{
				{"backend", "cheaper"},
				{"tier", "larger"},
			},
		},
		{
			name: "ignores leading whitespace noise on other lines",
			in:   "some preamble\n\n  unrelated: stuff\nneed-backend: alternate\n",
			want: []RecoveryHint{{"backend", "alternate"}},
		},
		{
			name: "unknown marker kind ignored",
			in:   "need-mood: cheerful\nneed-tier: larger\n",
			want: []RecoveryHint{{"tier", "larger"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRecoveryHints(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("want %d hints, got %d: %+v", len(tc.want), len(got), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("hint %d: want %+v, got %+v", i, tc.want[i], got[i])
				}
			}
		})
	}
}

func TestFormatRecoveryComment(t *testing.T) {
	if got := FormatRecoveryComment(nil); got != "" {
		t.Fatalf("empty hints should return empty string, got %q", got)
	}
	hints := []RecoveryHint{{"backend", "cheaper"}, {"tier", "larger"}}
	want := "need-backend: cheaper\nneed-tier: larger\n"
	if got := FormatRecoveryComment(hints); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
