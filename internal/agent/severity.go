package agent

import "strings"

// ExtractSeverity attempts to extract severity from finding text.
// Returns "blocking" if the text contains blocking markers, "advisory" otherwise.
func ExtractSeverity(text string) string {
	lower := strings.ToLower(text)
	blockingMarkers := []string{"[must]", "[blocking]", "severity: blocking", "🔴"}
	for _, m := range blockingMarkers {
		if strings.Contains(lower, m) {
			return "blocking"
		}
	}
	return "advisory"
}

// ExtractPrefix attempts to extract a prefix tag from finding text.
// Returns the matched prefix or empty string.
func ExtractPrefix(text string) string {
	prefixes := []string{"[must]", "[blocking]", "[imo]", "[nits]", "[fyi]", "[ask]"}
	lower := strings.ToLower(text)
	for _, p := range prefixes {
		if strings.Contains(lower, p) {
			return p
		}
	}
	return ""
}
