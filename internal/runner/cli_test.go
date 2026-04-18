package runner

import "testing"

func TestCLIArgs(t *testing.T) {
	cases := []struct {
		agent    string
		model    string
		wantName string
		wantArgs []string
		wantErr  bool
	}{
		{"", "", "claude", []string{"--print"}, false},
		{"claude", "sonnet", "claude", []string{"--print", "--model", "sonnet"}, false},
		{"opencode", "big-pickle", "opencode", []string{"run", "--dangerously-skip-permissions", "-m", "big-pickle"}, false},
		{"gemini", "gemini-2.5-pro", "gemini", []string{"-p", "-", "--yolo", "-m", "gemini-2.5-pro"}, false},
		{"bogus", "", "", nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			name, args, err := cliArgs(tc.agent, tc.model)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName {
				t.Fatalf("want name %q, got %q", tc.wantName, name)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("want %v args, got %v", tc.wantArgs, args)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("arg %d: want %q, got %q", i, tc.wantArgs[i], args[i])
				}
			}
		})
	}
}
