package main

import (
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// filterFindingsByIndices returns findings at the specified indices.
func filterFindingsByIndices(findings []domain.FindingGroup, indices []int) []domain.FindingGroup {
	indexSet := make(map[int]bool, len(indices))
	for _, i := range indices {
		indexSet[i] = true
	}

	result := make([]domain.FindingGroup, 0, len(indices))
	for i, f := range findings {
		if indexSet[i] {
			result = append(result, f)
		}
	}
	return result
}

// exitCodeError is a wrapper type for returning exit codes via error interface.
type exitCodeError struct {
	code domain.ExitCode
}

func (e exitCodeError) Error() string {
	switch e.code {
	case domain.ExitFindings:
		return "findings were reported"
	case domain.ExitError:
		return "review failed with error"
	case domain.ExitInterrupted:
		return "review was interrupted"
	default:
		return fmt.Sprintf("exit code %d", e.code)
	}
}

func exitCode(code domain.ExitCode) error {
	if code == domain.ExitNoFindings {
		return nil
	}
	return exitCodeError{code: code}
}

const maxStderrLines = 40

func formatFailedReviewerStderr(results []domain.ReviewerResult) string {
	var parts []string
	for _, r := range results {
		if r.Stderr == "" {
			continue
		}
		label := "failed"
		if r.AuthFailed {
			label = "auth failed"
		} else if r.TimedOut {
			label = "timed out"
		}

		header := fmt.Sprintf("Reviewer #%d (%s) [%s]:", r.ReviewerID, r.AgentName, label)

		lines := strings.Split(r.Stderr, "\n")
		var body string
		if len(lines) > maxStderrLines {
			body = fmt.Sprintf("... (last %d lines of captured output)\n", maxStderrLines) + strings.Join(lines[len(lines)-maxStderrLines:], "\n")
		} else {
			body = r.Stderr
		}

		parts = append(parts, header+"\n"+body)
	}
	return strings.Join(parts, "\n\n")
}
