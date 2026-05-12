package git

import (
	"fmt"
	"strconv"
	"strings"
)

// DiffSection represents a single file's diff within a unified diff output.
type DiffSection struct {
	FilePath   string // current file path (extracted from "b/..." in diff header)
	Content    string // full diff text for this file (including diff --git header)
	AddedLines int    // number of added lines (lines starting with "+", excluding header)
}

// ParseDiffSections parses a unified diff into per-file sections.
// It splits on "diff --git " boundaries and extracts file paths and line counts.
// For renamed files, FilePath is the destination (b/...) path.
// Binary files have AddedLines=0.
func ParseDiffSections(fullDiff string) []DiffSection {
	if fullDiff == "" {
		return nil
	}

	// Normalize CRLF → LF so that Windows-style or mixed line endings do not
	// affect splitting or path extraction. Section Content fields are returned
	// with LF-only line endings (callers that need original bytes should pass
	// LF-only input or handle re-encoding themselves).
	fullDiff = strings.ReplaceAll(fullDiff, "\r\n", "\n")

	// Split on "\ndiff --git " boundaries. The first element already begins
	// with "diff --git " (no leading newline), elements at index > 0 need it prepended.
	rawParts := strings.Split(fullDiff, "\ndiff --git ")
	if len(rawParts) == 0 {
		return nil
	}

	sections := make([]DiffSection, 0, len(rawParts))
	for i, part := range rawParts {
		if i > 0 {
			part = "diff --git " + part
		}
		if part == "" {
			continue
		}

		section := parseSingleSection(part)
		if section.FilePath == "" {
			continue
		}
		sections = append(sections, section)
	}

	return sections
}

// parseSingleSection parses one "diff --git ..." block into a DiffSection.
func parseSingleSection(content string) DiffSection {
	// Normalize CRLF → LF so that this function works correctly even when
	// called directly (not via ParseDiffSections which also normalizes).
	content = strings.ReplaceAll(content, "\r\n", "\n")

	lines := strings.Split(content, "\n")

	// First line is the "diff --git a/... b/..." header
	headerLine := lines[0]

	// Extract FilePath from the header. Git emits two forms:
	//   Unquoted: diff --git a/<path> b/<path>
	//   Quoted:   diff --git "a/<path>" "b/<path>"  (paths with spaces / non-ASCII)
	// For the quoted form, strconv.Unquote handles both space escapes and
	// git's octal byte escapes (e.g. \343\201\202 → UTF-8 あ).
	filePath := ""
	rest := strings.TrimPrefix(headerLine, "diff --git ")
	if strings.HasPrefix(rest, `"`) {
		// Quoted form: find the second quoted token (the "b/..." part).
		// The two tokens are separated by a space that follows the closing quote
		// of the first token. Find the closing quote of the first token.
		closeIdx := strings.Index(rest[1:], `" "`)
		if closeIdx >= 0 {
			// second token starts at closeIdx+3 (skip leading `" "`)
			bToken := rest[closeIdx+3:]
			// bToken is `"b/<path>"` — unquote it
			if unquoted, err := strconv.Unquote(bToken); err == nil {
				filePath = strings.TrimPrefix(unquoted, "b/")
			}
		}
	} else {
		// Unquoted form: use last " b/" to handle paths containing spaces
		bIdx := strings.LastIndex(headerLine, " b/")
		if bIdx >= 0 {
			filePath = headerLine[bIdx+3:] // skip " b/"
		}
	}
	// Trim any stray trailing \r (defensive; input is already CRLF-normalized above)
	filePath = strings.TrimRight(filePath, "\r")

	// Count added lines: lines starting with "+" but not "+++ " (diff header)
	addedLines := 0
	isBinary := false
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") &&
			strings.Contains(line, "Binary files") && strings.Contains(line, "differ") {
			isBinary = true
			break
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			addedLines++
		}
	}
	if isBinary {
		addedLines = 0
	}

	return DiffSection{
		FilePath:   filePath,
		Content:    content,
		AddedLines: addedLines,
	}
}

// JoinDiffSections concatenates the Content of multiple DiffSections.
// Returns empty string for nil/empty input.
func JoinDiffSections(sections []DiffSection) string {
	if len(sections) == 0 {
		return ""
	}
	parts := make([]string, len(sections))
	for i, s := range sections {
		parts[i] = s.Content
	}
	return strings.Join(parts, "\n")
}

// DiffGroup represents a group of diff sections assigned to one reviewer.
type DiffGroup struct {
	Key      string        // group identifier: "g01", "g02", ...
	Sections []DiffSection // files in this group
}

const (
	defaultMaxFilesPerGroup = 5
	defaultMaxLinesPerGroup = 300
	defaultMaxGroups        = 4
)

// GroupDiffSections splits DiffSections into groups respecting dual thresholds.
// maxFilesPerGroup: max files per group (default 5, 0 = use default)
// maxLinesPerGroup: max added lines per group (default 300, 0 = use default)
// maxGroups: max number of groups (default 4, 0 = use default)
func GroupDiffSections(sections []DiffSection, maxFilesPerGroup, maxLinesPerGroup, maxGroups int) []DiffGroup {
	if len(sections) == 0 {
		return nil
	}

	// Apply defaults
	if maxFilesPerGroup <= 0 {
		maxFilesPerGroup = defaultMaxFilesPerGroup
	}
	if maxLinesPerGroup <= 0 {
		maxLinesPerGroup = defaultMaxLinesPerGroup
	}
	if maxGroups <= 0 {
		maxGroups = defaultMaxGroups
	}

	// Greedy packing: build groups respecting both thresholds
	var groups []DiffGroup
	curFiles := 0
	curLines := 0
	curSections := []DiffSection{}

	for _, sec := range sections {
		wouldExceedFiles := curFiles+1 > maxFilesPerGroup
		wouldExceedLines := curLines+sec.AddedLines > maxLinesPerGroup

		if len(curSections) > 0 && (wouldExceedFiles || wouldExceedLines) {
			// Flush current group
			groups = append(groups, DiffGroup{Sections: curSections})
			curSections = []DiffSection{}
			curFiles = 0
			curLines = 0
		}

		curSections = append(curSections, sec)
		curFiles++
		curLines += sec.AddedLines
	}

	// Flush remaining
	if len(curSections) > 0 {
		groups = append(groups, DiffGroup{Sections: curSections})
	}

	// Merge groups until within maxGroups.
	// Strategy: pick the group with the fewest lines, then merge it with the
	// partner whose combined line count is closest to the target average
	// (totalLines / targetGroupCount). Partners that stay within the packing
	// thresholds (maxLinesPerGroup, maxFilesPerGroup) are preferred; threshold
	// exceedance is allowed only when no compliant partner exists.
	// This removes the adjacency bias of the previous algorithm — git diff
	// alphabetical order has no semantic meaning — and produces more balanced
	// group sizes while respecting packing thresholds where possible.
	lineCounts := make([]int, len(groups))
	totalLines := 0
	for i, g := range groups {
		lc := groupLineCount(g)
		lineCounts[i] = lc
		totalLines += lc
	}
	for len(groups) > maxGroups {
		targetAvg := totalLines / (len(groups) - 1)

		minIdx := 0
		for i := 1; i < len(groups); i++ {
			if lineCounts[i] < lineCounts[minIdx] {
				minIdx = i
			}
		}
		minLines := lineCounts[minIdx]

		bestPartner := -1
		bestDist := 0
		bestWithin := false
		for i := range groups {
			if i == minIdx {
				continue
			}
			combined := minLines + lineCounts[i]
			within := combined <= maxLinesPerGroup
			dist := combined - targetAvg
			if dist < 0 {
				dist = -dist
			}
			if bestPartner == -1 ||
				(within && !bestWithin) ||
				(within == bestWithin && dist < bestDist) {
				bestPartner = i
				bestDist = dist
				bestWithin = within
			}
		}

		lo, hi := minIdx, bestPartner
		if lo > hi {
			lo, hi = hi, lo
		}
		mergedSections := make([]DiffSection, 0, len(groups[lo].Sections)+len(groups[hi].Sections))
		mergedSections = append(mergedSections, groups[lo].Sections...)
		mergedSections = append(mergedSections, groups[hi].Sections...)
		merged := DiffGroup{Sections: mergedSections}
		mergedLC := lineCounts[lo] + lineCounts[hi]

		newGroups := make([]DiffGroup, 0, len(groups)-1)
		newGroups = append(newGroups, groups[:lo]...)
		newGroups = append(newGroups, merged)
		newGroups = append(newGroups, groups[lo+1:hi]...)
		newGroups = append(newGroups, groups[hi+1:]...)
		groups = newGroups

		newLC := make([]int, 0, len(lineCounts)-1)
		newLC = append(newLC, lineCounts[:lo]...)
		newLC = append(newLC, mergedLC)
		newLC = append(newLC, lineCounts[lo+1:hi]...)
		newLC = append(newLC, lineCounts[hi+1:]...)
		lineCounts = newLC
	}

	// Assign keys
	for i := range groups {
		groups[i].Key = fmt.Sprintf("g%02d", i+1)
	}

	return groups
}

// groupLineCount returns the total added lines in a DiffGroup.
func groupLineCount(g DiffGroup) int {
	total := 0
	for _, s := range g.Sections {
		total += s.AddedLines
	}
	return total
}
