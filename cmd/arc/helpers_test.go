package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masa6161/arc-cli/internal/domain"
)

func TestExitCodeError_Error(t *testing.T) {
	tests := []struct {
		code     domain.ExitCode
		contains string
	}{
		{domain.ExitFindings, "findings were reported"},
		{domain.ExitError, "review failed with error"},
		{domain.ExitInterrupted, "review was interrupted"},
		{domain.ExitCode(99), "exit code 99"},
	}

	for _, tt := range tests {
		t.Run(tt.contains, func(t *testing.T) {
			err := exitCodeError{code: tt.code}
			if err.Error() != tt.contains {
				t.Errorf("expected %q, got %q", tt.contains, err.Error())
			}
		})
	}
}

func TestExitCode_ReturnsNilForNoFindings(t *testing.T) {
	err := exitCode(domain.ExitNoFindings)
	if err != nil {
		t.Errorf("expected nil for ExitNoFindings, got %v", err)
	}
}

func TestExitCode_ReturnsErrorForOtherCodes(t *testing.T) {
	codes := []domain.ExitCode{
		domain.ExitFindings,
		domain.ExitError,
		domain.ExitInterrupted,
	}

	for _, code := range codes {
		err := exitCode(code)
		if err == nil {
			t.Errorf("expected error for code %d, got nil", code)
		}
		exitErr, ok := err.(exitCodeError)
		if !ok {
			t.Errorf("expected exitCodeError type, got %T", err)
		}
		if exitErr.code != code {
			t.Errorf("expected code %d, got %d", code, exitErr.code)
		}
	}
}

func TestFormatFailedReviewerStderr_AllWithStderr(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", Stderr: "error: something broke"},
		{ReviewerID: 2, AgentName: "claude", Stderr: "fatal: auth token expired"},
	}

	got := formatFailedReviewerStderr(results)

	if !strings.Contains(got, "Reviewer #1") {
		t.Errorf("expected output to contain 'Reviewer #1', got %q", got)
	}
	if !strings.Contains(got, "codex") {
		t.Errorf("expected output to contain 'codex', got %q", got)
	}
	if !strings.Contains(got, "Reviewer #2") {
		t.Errorf("expected output to contain 'Reviewer #2', got %q", got)
	}
	if !strings.Contains(got, "claude") {
		t.Errorf("expected output to contain 'claude', got %q", got)
	}
	if !strings.Contains(got, "error: something broke") {
		t.Errorf("expected output to contain stderr content, got %q", got)
	}
	if !strings.Contains(got, "fatal: auth token expired") {
		t.Errorf("expected output to contain stderr content, got %q", got)
	}
}

func TestFormatFailedReviewerStderr_SkipsEmptyStderr(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", Stderr: "error: something broke"},
		{ReviewerID: 2, AgentName: "claude", Stderr: ""},
		{ReviewerID: 3, AgentName: "gemini", Stderr: "timeout exceeded"},
	}

	got := formatFailedReviewerStderr(results)

	if !strings.Contains(got, "Reviewer #1") {
		t.Errorf("expected output to contain 'Reviewer #1', got %q", got)
	}
	if strings.Contains(got, "Reviewer #2") {
		t.Errorf("expected output to skip 'Reviewer #2' (empty stderr), got %q", got)
	}
	if !strings.Contains(got, "Reviewer #3") {
		t.Errorf("expected output to contain 'Reviewer #3', got %q", got)
	}
}

func TestFormatFailedReviewerStderr_TruncatesLongStderr(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	longStderr := strings.Join(lines, "\n")

	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", Stderr: longStderr},
	}

	got := formatFailedReviewerStderr(results)

	if !strings.Contains(got, "last 40 lines of captured output") {
		t.Errorf("expected truncation message, got %q", got)
	}
	if !strings.Contains(got, "line 59") {
		t.Errorf("expected last line to be present, got %q", got)
	}
	if strings.Contains(got, "line 0\n") {
		t.Errorf("expected early lines to be truncated, got %q", got)
	}
}

func TestFormatFailedReviewerStderr_ExactMaxLinesNotTruncated(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	stderrExact := strings.Join(lines, "\n")

	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", Stderr: stderrExact},
	}

	got := formatFailedReviewerStderr(results)

	if strings.Contains(got, "last 40 lines of captured output") {
		t.Errorf("exactly 40 lines should NOT trigger truncation, got %q", got)
	}
	if !strings.Contains(got, "line 0") {
		t.Errorf("expected first line to be present, got %q", got)
	}
	if !strings.Contains(got, "line 39") {
		t.Errorf("expected last line to be present, got %q", got)
	}
}

func TestFormatFailedReviewerStderr_AllEmpty(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", Stderr: ""},
		{ReviewerID: 2, AgentName: "claude", Stderr: ""},
	}

	got := formatFailedReviewerStderr(results)

	if got != "" {
		t.Errorf("expected empty string when all stderr empty, got %q", got)
	}
}

func TestFormatFailedReviewerStderr_FailureTypeLabels(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", AuthFailed: true, Stderr: "auth error"},
		{ReviewerID: 2, AgentName: "claude", TimedOut: true, Stderr: "timed out waiting"},
		{ReviewerID: 3, AgentName: "gemini", Stderr: "generic failure"},
	}

	got := formatFailedReviewerStderr(results)

	if !strings.Contains(got, "[auth failed]") {
		t.Errorf("expected '[auth failed]' label for AuthFailed reviewer, got %q", got)
	}
	if !strings.Contains(got, "[timed out]") {
		t.Errorf("expected '[timed out]' label for TimedOut reviewer, got %q", got)
	}
	if !strings.Contains(got, "[failed]") {
		t.Errorf("expected '[failed]' label for generic failure reviewer, got %q", got)
	}
}
