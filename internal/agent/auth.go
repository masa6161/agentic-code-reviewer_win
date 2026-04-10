package agent

import (
	"runtime"
	"slices"
	"strings"
)

// authExitCodes maps agent names to known authentication failure exit codes.
var authExitCodes = map[string][]int{
	"gemini": {41},
}

// authStderrPatterns contains substrings that indicate authentication failure
// when found in stderr output (checked case-insensitively).
var authStderrPatterns = []string{
	"api_key",
	"unauthorized",
	"401",
	"authentication required",
	"invalid credentials",
}

// authHints maps agent names to actionable error messages shown on auth failure.
var authHints = map[string]string{
	"gemini": "Set GEMINI_API_KEY or run 'gemini auth login' to authenticate.",
	"claude": "Run 'claude login' or check your API key configuration.",
	"codex":  "Set OPENAI_API_KEY or run 'codex auth' to authenticate.",
}

var nonAuthStartupPatterns = []string{
	"spawn eperm",
	"uv_spawn",
	"failed to relaunch",
	"operation not permitted",
	"access is denied",
}

func containsAuthSignal(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, pattern := range authStderrPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func containsNonAuthStartupSignal(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, pattern := range nonAuthStartupPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// IsAuthFailure returns true if the given exit code and stderr indicate
// an authentication failure for the named agent. Exit code 0 is never
// considered an auth failure.
func IsAuthFailure(agentName string, exitCode int, stderr string) bool {
	if exitCode == 0 {
		return false
	}

	if codes, ok := authExitCodes[agentName]; ok {
		if slices.Contains(codes, exitCode) {
			// Some Windows CLI launch failures reuse the same exit code as auth errors.
			// Preserve known auth exit codes unless stderr clearly indicates a
			// startup/spawn failure unrelated to authentication.
			if runtime.GOOS == "windows" {
				if containsAuthSignal(stderr) {
					return true
				}
				return !containsNonAuthStartupSignal(stderr)
			}
			return true
		}
	}

	return containsAuthSignal(stderr)
}

// AuthHint returns an actionable error message for the named agent.
// Returns a generic hint for unknown agents.
func AuthHint(agentName string) string {
	if hint, ok := authHints[agentName]; ok {
		return hint
	}
	return "Check your authentication configuration for " + agentName + "."
}
