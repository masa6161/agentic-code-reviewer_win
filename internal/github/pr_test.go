package github

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestClassifyGHError_NoPRFound(t *testing.T) {
	// Create an ExitError with stderr indicating no PR found
	exitErr := &exec.ExitError{
		Stderr: []byte(`no pull requests found for branch "feature-branch"`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrNoPRFound) {
		t.Errorf("expected ErrNoPRFound, got %v", err)
	}
}

func TestClassifyGHError_AuthFailed401(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`HTTP 401: Bad credentials (https://api.github.com/graphql)`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_AuthFailedLogin(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`To get started with GitHub CLI, please run:  gh auth login`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_AuthFailedCredentials(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`error: authentication required, check your credentials`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_OtherError(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`repository not found`),
	}

	err := classifyGHError(exitErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected generic error, got %v", err)
	}
	if err == nil {
		t.Error("expected an error, got nil")
	}
}

func TestClassifyGHError_EmptyStderr(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte{},
	}

	err := classifyGHError(exitErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected generic error for empty stderr, got %v", err)
	}
}

func TestClassifyGHError_NonExitError(t *testing.T) {
	// Test with a non-ExitError
	plainErr := errors.New("some other error")

	err := classifyGHError(plainErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected wrapped error for non-ExitError, got %v", err)
	}
}

func TestParsePRViewJSON_ValidResponse(t *testing.T) {
	json := `{"headRefName": "feature-branch", "baseRefName": "main"}`

	head, base, err := parsePRViewJSON([]byte(json))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "feature-branch" {
		t.Errorf("expected head 'feature-branch', got %q", head)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got %q", base)
	}
}

func TestParsePRViewJSON_InvalidJSON(t *testing.T) {
	_, _, err := parsePRViewJSON([]byte(`not valid json`))

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePRViewJSON_MissingFields(t *testing.T) {
	json := `{"headRefName": "feature-branch"}`

	head, base, err := parsePRViewJSON([]byte(json))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "feature-branch" {
		t.Errorf("expected head 'feature-branch', got %q", head)
	}
	if base != "" {
		t.Errorf("expected empty base, got %q", base)
	}
}

func TestUrlMatches_HTTPSFormat(t *testing.T) {
	result := urlMatches("https://github.com/owner/repo.git", "https://github.com/owner/repo")
	if !result {
		t.Error("expected true for HTTPS URLs with/without .git suffix")
	}
}

func TestUrlMatches_SSHShorthandFormat(t *testing.T) {
	result := urlMatches("git@github.com:owner/repo.git", "https://github.com/owner/repo")
	if !result {
		t.Error("expected true for SSH shorthand vs HTTPS")
	}
}

func TestUrlMatches_SSHURLFormat(t *testing.T) {
	result := urlMatches("ssh://git@github.com/owner/repo.git", "https://github.com/owner/repo")
	if !result {
		t.Error("expected true for ssh:// URL vs HTTPS")
	}
}

func TestUrlMatches_SSHURLFormatNoUser(t *testing.T) {
	result := urlMatches("ssh://github.com/owner/repo", "https://github.com/owner/repo")
	if !result {
		t.Error("expected true for ssh:// URL without user vs HTTPS")
	}
}

func TestUrlMatches_SSHURLVsSSHShorthand(t *testing.T) {
	result := urlMatches("ssh://git@github.com/owner/repo.git", "git@github.com:owner/repo.git")
	if !result {
		t.Error("expected true for ssh:// URL vs SSH shorthand")
	}
}

func TestUrlMatches_DifferentRepos(t *testing.T) {
	result := urlMatches("https://github.com/owner/repo1", "https://github.com/owner/repo2")
	if result {
		t.Error("expected false for different repos")
	}
}

func TestUrlMatches_CaseInsensitive(t *testing.T) {
	result := urlMatches("https://github.com/OWNER/REPO", "https://github.com/owner/repo")
	if !result {
		t.Error("expected true for case-insensitive comparison")
	}
}

func TestIsGHAvailable(t *testing.T) {
	result := IsGHAvailable()
	_ = result
}

func TestCheckGHAvailable(t *testing.T) {
	err := CheckGHAvailable()
	if err != nil {
		if !strings.Contains(err.Error(), "gh CLI not available") {
			t.Errorf("expected descriptive error, got %q", err.Error())
		}
	}
}

func TestParsePRViewJSON_EmptyJSON(t *testing.T) {
	head, base, err := parsePRViewJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "" {
		t.Errorf("expected empty head, got %q", head)
	}
	if base != "" {
		t.Errorf("expected empty base, got %q", base)
	}
}
