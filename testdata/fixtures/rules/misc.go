package rules

import "go.temporal.io/sdk/workflow"

// Service shows that a method can be a workflow.
type Service struct{}

func (s *Service) MethodWorkflow(ctx workflow.Context, x int) (string, error) {
	if x > 0 {
		return "yes", nil
	}
	return "no", nil
}

// Not workflows:

func unexportedWorkflow(ctx workflow.Context) (string, error) { return "", nil }

func NoErrorResult(ctx workflow.Context) string { return "" }

func NoContext(x int) (string, error) { return "", nil }

var _ = unexportedWorkflow

// TwelveIfs has 2^12 = 4096 paths, more than the 2000 cap.
func TwelveIfs(ctx workflow.Context, f [12]bool) (int, error) {
	n := 0
	if f[0] {
		n++
	}
	if f[1] {
		n++
	}
	if f[2] {
		n++
	}
	if f[3] {
		n++
	}
	if f[4] {
		n++
	}
	if f[5] {
		n++
	}
	if f[6] {
		n++
	}
	if f[7] {
		n++
	}
	if f[8] {
		n++
	}
	if f[9] {
		n++
	}
	if f[10] {
		n++
	}
	if f[11] {
		n++
	}
	return n, nil
}
