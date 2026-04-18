package dispatch

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// kubectlApply pipes manifest to `kubectl apply -f -` in namespace ns.
func kubectlApply(ns, manifest string) error {
	cmd := exec.Command("kubectl", "-n", ns, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}
