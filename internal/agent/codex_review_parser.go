package agent

import (
	"bufio"
	"encoding/json"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

const (
	// scannerInitialBuffer is the initial buffer size for the scanner (64KB).
	scannerInitialBuffer = 64 * 1024
	// scannerMaxLineSize is the maximum line size the scanner will handle (10MB).
	scannerMaxLineSize = 10 * 1024 * 1024
)

// CodexOutputParser parses JSONL output from the codex CLI.
type CodexOutputParser struct {
	reviewerID  int
	lineNum     int
	parseErrors int
}

// NewCodexOutputParser creates a new parser for codex output.
func NewCodexOutputParser(reviewerID int) *CodexOutputParser {
	return &CodexOutputParser{
		reviewerID: reviewerID,
	}
}

// ReadFinding reads and parses the next finding from the codex output stream.
// Codex outputs JSONL format with items like:
//
//	{"item": {"type": "agent_message", "text": "finding description"}}
//
// Returns a finding when one is found.
// Returns (nil, nil) when no more findings are available (end of stream).
// Returns (nil, error) for fatal scanner errors - caller should stop.
// Non-JSON lines (e.g. codex CLI status messages) are silently skipped.
func (p *CodexOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	for scanner.Scan() {
		p.lineNum++
		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse the JSONL event; skip non-JSON lines (CLI status messages etc.)
		var event struct {
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}

		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if len(line) > 0 && line[0] == '{' {
				p.parseErrors++
			}
			continue
		}

		// Only process agent_message items with non-empty, actionable text
		if event.Item.Type == "agent_message" && event.Item.Text != "" &&
			!IsNonFindingText(event.Item.Text) {
			text := event.Item.Text
			return &domain.Finding{
				Text:       text,
				ReviewerID: p.reviewerID,
				Severity:   ExtractSeverity(text),
				Prefix:     ExtractPrefix(text),
			}, nil
		}
		// Valid JSON but not a finding - continue to next line
	}

	// Check for scanner error
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// No more findings
	return nil, nil
}

// ParseErrors returns the number of recoverable parse errors encountered.
func (p *CodexOutputParser) ParseErrors() int {
	return p.parseErrors
}

// ConfigureScanner configures a bufio.Scanner with appropriate buffer sizes
// for parsing codex output (64KB initial, 100MB max).
func ConfigureScanner(scanner *bufio.Scanner) {
	scanner.Buffer(make([]byte, 0, scannerInitialBuffer), scannerMaxLineSize)
}
