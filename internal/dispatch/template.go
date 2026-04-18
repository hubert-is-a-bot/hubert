package dispatch

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed templates/job.yaml
var jobTemplate string

// jobData holds all template inputs for a single runner Job.
type jobData struct {
	JobName               string
	Namespace             string
	Image                 string
	ServiceAccount        string
	RunID                 string
	Repo                  string
	Issue                 int
	PR                    int
	Agent                 string
	Model                 string
	Mode                  string
	Role                  string
	Branch                string
	Iteration             int
	BudgetUSD             float64
	CPURequest            string
	CPULimit              string
	MemoryRequest         string
	MemoryLimit           string
	ActiveDeadlineSeconds int
}

var parsedJobTemplate = func() *template.Template {
	t, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		panic(fmt.Sprintf("dispatch: parse job template: %v", err))
	}
	return t
}()

// renderJob returns the YAML manifest for a single runner Job.
func renderJob(d jobData) (string, error) {
	var buf bytes.Buffer
	if err := parsedJobTemplate.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("render job template: %w", err)
	}
	return buf.String(), nil
}
