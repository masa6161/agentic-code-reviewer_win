// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoPRFound indicates no pull request exists for the given branch.
var ErrNoPRFound = errors.New("no pull request found")

// ErrAuthFailed indicates GitHub authentication failed.
var ErrAuthFailed = errors.New("GitHub authentication failed")

// GetCurrentPRNumber returns the PR number for the given branch (or current branch).
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed,
// or another error for other failures.
func GetCurrentPRNumber(ctx context.Context, branch string) (string, error) {
	args := []string{"pr", "view"}
	if branch != "" {
		args = append(args, branch)
	}
	args = append(args, "--json", "number", "--jq", ".number")

	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

// prViewResponse represents the JSON response from gh pr view.
type prViewResponse struct {
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
}

// parsePRViewJSON parses the JSON output from gh pr view.
func parsePRViewJSON(data []byte) (head, base string, err error) {
	var resp prViewResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("failed to parse PR view response: %w", err)
	}
	return resp.HeadRefName, resp.BaseRefName, nil
}

// GetPRBranch returns the head branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBranch(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	head, _, err := parsePRViewJSON(out)
	return head, err
}

// GetPRBaseRef returns the base branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBaseRef(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "baseRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	_, base, err := parsePRViewJSON(out)
	return base, err
}

// ValidatePR checks that a PR exists and is accessible.
// Returns nil if the PR exists, or a descriptive error:
// - ErrNoPRFound if the PR doesn't exist
// - ErrAuthFailed if authentication failed
// - Other error for unexpected failures
func ValidatePR(ctx context.Context, prNumber string) error {
	// Use gh pr view with minimal fields to validate the PR exists
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "number")
	_, err := cmd.Output()
	if err != nil {
		return classifyGHError(err)
	}
	return nil
}

// GetRepoRemote returns the git remote name that corresponds to the current gh repo.
// This handles fork workflows where "origin" may point to a fork, not the base repo.
// Falls back to "origin" if detection fails.
func GetRepoRemote(ctx context.Context) string {
	// Get the current repo's SSH and HTTPS URLs from gh
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "url,sshUrl")
	out, err := cmd.Output()
	if err != nil {
		return "origin" // fallback
	}

	var repoInfo struct {
		URL    string `json:"url"`
		SSHUrl string `json:"sshUrl"`
	}
	if err := json.Unmarshal(out, &repoInfo); err != nil {
		return "origin"
	}

	// Get git remotes
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "-v")
	remoteOut, err := remoteCmd.Output()
	if err != nil {
		return "origin"
	}

	// Parse remotes and find matching one
	// Format: "origin	git@github.com:owner/repo.git (fetch)"
	lines := strings.Split(string(remoteOut), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		remoteName := fields[0]
		remoteURL := fields[1]

		// Check if this remote matches either URL (normalized)
		if urlMatches(remoteURL, repoInfo.URL) || urlMatches(remoteURL, repoInfo.SSHUrl) {
			return remoteName
		}
	}

	return "origin"
}

// urlMatches checks if two git URLs refer to the same repository.
// Handles HTTPS, SSH shorthand (git@host:path), and SSH URL (ssh://host/path) variations.
func urlMatches(url1, url2 string) bool {
	// Normalize URLs for comparison
	normalize := func(url string) string {
		url = strings.TrimSuffix(url, ".git")
		url = strings.TrimPrefix(url, "https://")
		url = strings.TrimPrefix(url, "http://")
		// Handle SSH URL format: ssh://git@github.com/owner/repo -> github.com/owner/repo
		if strings.HasPrefix(url, "ssh://") {
			url = strings.TrimPrefix(url, "ssh://")
			// Remove user@ if present (e.g., git@github.com -> github.com)
			if idx := strings.Index(url, "@"); idx != -1 {
				url = url[idx+1:]
			}
		}
		// Handle SSH shorthand format: git@github.com:owner/repo -> github.com/owner/repo
		if strings.HasPrefix(url, "git@") {
			url = strings.TrimPrefix(url, "git@")
			url = strings.Replace(url, ":", "/", 1)
		}
		return strings.ToLower(url)
	}
	return normalize(url1) == normalize(url2)
}

// classifyGHError examines a gh CLI error and returns a typed error.
func classifyGHError(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("gh command failed: %w", err)
	}

	stderr := strings.ToLower(string(exitErr.Stderr))

	if strings.Contains(stderr, "no pull request") {
		return ErrNoPRFound
	}

	if strings.Contains(stderr, "401") ||
		strings.Contains(stderr, "auth") ||
		strings.Contains(stderr, "credentials") ||
		strings.Contains(stderr, "login") {
		return ErrAuthFailed
	}

	errMsg := strings.TrimSpace(string(exitErr.Stderr))
	if errMsg != "" {
		return fmt.Errorf("gh command failed: %s", errMsg)
	}
	return fmt.Errorf("gh command failed: %w", err)
}

// IsGHAvailable checks if the gh CLI is available.
func IsGHAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// CheckGHAvailable returns an error if the gh CLI is not available.
func CheckGHAvailable() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not available: %w", err)
	}
	return nil
}

