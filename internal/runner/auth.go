package runner

import (
	"context"
	"fmt"
	"os/exec"
)

// ConfigureGitAuth rewrites https://github.com/ URLs to embed the
// given PAT so that clone and push operations authenticate
// without a credential helper. Called once per Job at runner
// startup when HUBERT_GH_TOKEN is present.
//
// We rewrite via `git config --global url.<N>.insteadOf` rather
// than putting the token in the clone URL directly, so the
// token never appears in command-line args (stops accidental
// exposure in process listings and logs of later git commands).
func ConfigureGitAuth(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	rewrite := fmt.Sprintf("https://x-access-token:%s@github.com/", token)
	cmd := exec.CommandContext(ctx, "git", "config", "--global",
		"url."+rewrite+".insteadOf", "https://github.com/")
	return cmd.Run()
}
