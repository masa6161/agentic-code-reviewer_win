package agent

import "testing"

func TestExtractSeverity_BlockingMarkers(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"must tag", "[must] fix this null check", "blocking"},
		{"blocking tag", "[blocking] missing auth", "blocking"},
		{"severity marker", "severity: blocking - SQL injection", "blocking"},
		{"emoji marker", "🔴 critical bug found", "blocking"},
		{"case insensitive", "[MUST] uppercase tag", "blocking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSeverity(tt.text)
			if got != tt.want {
				t.Errorf("ExtractSeverity(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractSeverity_DefaultAdvisory(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"plain text", "consider using a mutex here"},
		{"imo tag", "[imo] this could be cleaner"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSeverity(tt.text)
			if got != "advisory" {
				t.Errorf("ExtractSeverity(%q) = %q, want \"advisory\"", tt.text, got)
			}
		})
	}
}

func TestExtractPrefix_MatchesTags(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"[must] fix this", "[must]"},
		{"[imo] consider this", "[imo]"},
		{"[nits] formatting issue", "[nits]"},
		{"[fyi] for your information", "[fyi]"},
		{"[ask] what is the intent here?", "[ask]"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := ExtractPrefix(tt.text)
			if got != tt.want {
				t.Errorf("ExtractPrefix(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractPrefix_NoMatch(t *testing.T) {
	got := ExtractPrefix("plain finding text without tags")
	if got != "" {
		t.Errorf("ExtractPrefix() = %q, want empty string", got)
	}
}
