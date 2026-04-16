package git

import (
	"fmt"
	"strings"
	"testing"
)

// --- Test helpers / fixtures ---

// makeFileDiff builds a minimal but realistic unified diff block for one file.
// addedCount controls how many "+ added line" lines are included.
func makeFileDiff(oldPath, newPath string, addedCount int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "diff --git a/%s b/%s\n", oldPath, newPath)
	sb.WriteString("index abc1234..def5678 100644\n")
	fmt.Fprintf(&sb, "--- a/%s\n", oldPath)
	fmt.Fprintf(&sb, "+++ b/%s\n", newPath)
	sb.WriteString("@@ -1,3 +1,5 @@\n")
	sb.WriteString(" package main\n")
	for i := 0; i < addedCount; i++ {
		fmt.Fprintf(&sb, "+added line %d\n", i+1)
	}
	return sb.String()
}

// makeFileDiffSimple builds a diff with a fixed small addition for basic tests.
func makeFileDiffSimple(name string) string {
	return `diff --git a/` + name + ` b/` + name + `
index abc1234..def5678 100644
--- a/` + name + `
+++ b/` + name + `
@@ -1,3 +1,5 @@
 package main

+import "fmt"
+
 func main() {`
}

// reusable diff samples
const basicDiff3Files = `diff --git a/file1.go b/file1.go
index abc1234..def5678 100644
--- a/file1.go
+++ b/file1.go
@@ -1,3 +1,5 @@
 package main

+import "fmt"
+
 func main() {
diff --git a/file2.go b/file2.go
index aaa..bbb 100644
--- a/file2.go
+++ b/file2.go
@@ -1,2 +1,3 @@
 package main
+// comment
diff --git a/file3.go b/file3.go
index ccc..ddd 100644
--- a/file3.go
+++ b/file3.go
@@ -1,2 +1,4 @@
 package main
+// line1
+// line2
+// line3`

const renameDiff = `diff --git a/old_name.go b/new_name.go
similarity index 90%
rename from old_name.go
rename to new_name.go
index abc..def 100644
--- a/old_name.go
+++ b/new_name.go
@@ -1,3 +1,4 @@
 package main
+// renamed`

const binaryDiff = `diff --git a/image.png b/image.png
new file mode 100644
index 0000000..abc1234
Binary files /dev/null and b/image.png differ`

// --- ParseDiffSections tests ---

func TestParseDiffSections_BasicMultiFile(t *testing.T) {
	sections := ParseDiffSections(basicDiff3Files)

	if len(sections) != 3 {
		t.Fatalf("ParseDiffSections() returned %d sections, want 3", len(sections))
	}

	tests := []struct {
		filePath   string
		addedLines int
	}{
		{"file1.go", 2},
		{"file2.go", 1},
		{"file3.go", 3},
	}

	for i, want := range tests {
		got := sections[i]
		if got.FilePath != want.filePath {
			t.Errorf("section[%d].FilePath = %q, want %q", i, got.FilePath, want.filePath)
		}
		if got.AddedLines != want.addedLines {
			t.Errorf("section[%d].AddedLines = %d, want %d", i, got.AddedLines, want.addedLines)
		}
		if !strings.HasPrefix(got.Content, "diff --git ") {
			t.Errorf("section[%d].Content should start with 'diff --git ', got: %q", i, got.Content[:min(50, len(got.Content))])
		}
	}
}

func TestParseDiffSections_RenamedFile(t *testing.T) {
	sections := ParseDiffSections(renameDiff)

	if len(sections) != 1 {
		t.Fatalf("ParseDiffSections() returned %d sections, want 1", len(sections))
	}

	got := sections[0]
	if got.FilePath != "new_name.go" {
		t.Errorf("FilePath = %q, want %q", got.FilePath, "new_name.go")
	}
	if got.AddedLines != 1 {
		t.Errorf("AddedLines = %d, want 1", got.AddedLines)
	}
}

func TestParseDiffSections_BinaryFile(t *testing.T) {
	sections := ParseDiffSections(binaryDiff)

	if len(sections) != 1 {
		t.Fatalf("ParseDiffSections() returned %d sections, want 1", len(sections))
	}

	got := sections[0]
	if got.FilePath != "image.png" {
		t.Errorf("FilePath = %q, want %q", got.FilePath, "image.png")
	}
	if got.AddedLines != 0 {
		t.Errorf("AddedLines = %d, want 0 for binary file", got.AddedLines)
	}
}

func TestParseDiffSections_EmptyInput(t *testing.T) {
	sections := ParseDiffSections("")
	if sections != nil {
		t.Errorf("ParseDiffSections(\"\") = %v, want nil", sections)
	}
}

func TestParseDiffSections_SingleFile(t *testing.T) {
	input := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
 package main
+// added`

	sections := ParseDiffSections(input)

	if len(sections) != 1 {
		t.Fatalf("ParseDiffSections() returned %d sections, want 1", len(sections))
	}

	got := sections[0]
	if got.FilePath != "main.go" {
		t.Errorf("FilePath = %q, want %q", got.FilePath, "main.go")
	}
	if got.AddedLines != 1 {
		t.Errorf("AddedLines = %d, want 1", got.AddedLines)
	}
}

func TestParseDiffSections_PathWithSpaces(t *testing.T) {
	input := `diff --git a/path with space/file.go b/path with space/file.go
index abc..def 100644
--- a/path with space/file.go
+++ b/path with space/file.go
@@ -1,2 +1,3 @@
 package main
+// added`

	sections := ParseDiffSections(input)

	if len(sections) != 1 {
		t.Fatalf("ParseDiffSections() returned %d sections, want 1", len(sections))
	}

	got := sections[0]
	if got.FilePath != "path with space/file.go" {
		t.Errorf("FilePath = %q, want %q", got.FilePath, "path with space/file.go")
	}
}

// --- CRLF and quoted-path robustness tests ---

func TestParseDiffSections_CRLF(t *testing.T) {
	// Build a 3-section diff joined with \r\n line endings.
	lf := basicDiff3Files
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")

	sections := ParseDiffSections(crlf)
	if len(sections) != 3 {
		t.Fatalf("ParseDiffSections(CRLF) returned %d sections, want 3", len(sections))
	}
	want := []string{"file1.go", "file2.go", "file3.go"}
	for i, w := range want {
		if sections[i].FilePath != w {
			t.Errorf("section[%d].FilePath = %q, want %q", i, sections[i].FilePath, w)
		}
	}
}

func TestParseSingleSection_CRLFHeader(t *testing.T) {
	// Header line ends with \r\n; extracted path must have no trailing \r.
	content := "diff --git a/foo.go b/foo.go\r\nindex abc..def 100644\r\n--- a/foo.go\r\n+++ b/foo.go\r\n@@ -1 +1,2 @@\r\n package main\r\n+// added\r\n"
	section := parseSingleSection(content)
	if section.FilePath != "foo.go" {
		t.Errorf("FilePath = %q, want %q", section.FilePath, "foo.go")
	}
}

func TestParseSingleSection_QuotedPathASCII(t *testing.T) {
	// Git emits quoted form for paths containing spaces.
	content := "diff --git \"a/has space.txt\" \"b/has space.txt\"\nindex abc..def 100644\n--- \"a/has space.txt\"\n+++ \"b/has space.txt\"\n@@ -1 +1,2 @@\n line\n+added\n"
	section := parseSingleSection(content)
	if section.FilePath != "has space.txt" {
		t.Errorf("FilePath = %q, want %q", section.FilePath, "has space.txt")
	}
	if section.AddedLines != 1 {
		t.Errorf("AddedLines = %d, want 1", section.AddedLines)
	}
}

func TestParseSingleSection_QuotedPathMultibyte(t *testing.T) {
	// Git's octal-escaped Japanese filename: あ.txt (UTF-8: \343\201\202)
	// git emits: diff --git "a/\343\201\202.txt" "b/\343\201\202.txt"
	content := "diff --git \"a/\\343\\201\\202.txt\" \"b/\\343\\201\\202.txt\"\nindex abc..def 100644\n--- \"a/\\343\\201\\202.txt\"\n+++ \"b/\\343\\201\\202.txt\"\n@@ -1 +1,2 @@\n line\n+added\n"
	section := parseSingleSection(content)
	want := "\u3042.txt" // あ.txt
	if section.FilePath != want {
		t.Errorf("FilePath = %q, want %q", section.FilePath, want)
	}
	if section.AddedLines != 1 {
		t.Errorf("AddedLines = %d, want 1", section.AddedLines)
	}
}

// --- JoinDiffSections tests ---

func TestJoinDiffSections(t *testing.T) {
	t.Run("empty input returns empty string", func(t *testing.T) {
		result := JoinDiffSections(nil)
		if result != "" {
			t.Errorf("JoinDiffSections(nil) = %q, want %q", result, "")
		}
		result = JoinDiffSections([]DiffSection{})
		if result != "" {
			t.Errorf("JoinDiffSections([]) = %q, want %q", result, "")
		}
	})

	t.Run("single section returns its content", func(t *testing.T) {
		s := DiffSection{Content: "diff --git a/x b/x\n+line"}
		result := JoinDiffSections([]DiffSection{s})
		if result != s.Content {
			t.Errorf("JoinDiffSections([single]) = %q, want %q", result, s.Content)
		}
	})

	t.Run("multiple sections produce valid diff text", func(t *testing.T) {
		sections := ParseDiffSections(basicDiff3Files)
		joined := JoinDiffSections(sections)

		// Joined result should contain all three files
		for _, name := range []string{"file1.go", "file2.go", "file3.go"} {
			if !strings.Contains(joined, "b/"+name) {
				t.Errorf("JoinDiffSections() result missing %q", name)
			}
		}
		// Should start with diff --git
		if !strings.HasPrefix(joined, "diff --git ") {
			t.Errorf("joined diff should start with 'diff --git ', got %q", joined[:min(50, len(joined))])
		}
	})
}

// --- GroupDiffSections tests ---

// makeSections creates n DiffSections each with addedLines added lines.
func makeSections(n, addedLines int) []DiffSection {
	sections := make([]DiffSection, n)
	for i := range sections {
		sections[i] = DiffSection{
			FilePath:   fmt.Sprintf("file%d.go", i+1),
			Content:    fmt.Sprintf("diff --git a/file%d.go b/file%d.go\n+line", i+1, i+1),
			AddedLines: addedLines,
		}
	}
	return sections
}

func TestGroupDiffSections_FileThreshold(t *testing.T) {
	// 12 files, each 10 lines, max 5 files per group → ceil(12/5) = 3 groups
	sections := makeSections(12, 10)
	groups := GroupDiffSections(sections, 5, 300, 10)

	if len(groups) != 3 {
		t.Errorf("GroupDiffSections() returned %d groups, want 3", len(groups))
	}

	// First two groups should have 5 files each, last has 2
	if len(groups[0].Sections) != 5 {
		t.Errorf("group[0] has %d sections, want 5", len(groups[0].Sections))
	}
	if len(groups[1].Sections) != 5 {
		t.Errorf("group[1] has %d sections, want 5", len(groups[1].Sections))
	}
	if len(groups[2].Sections) != 2 {
		t.Errorf("group[2] has %d sections, want 2", len(groups[2].Sections))
	}
}

func TestGroupDiffSections_LineThreshold(t *testing.T) {
	// 3 files, each 200 lines, max 300 lines per group → file1 (200) alone, file2+file3 can't fit together
	// file1: 200 lines → group1
	// file2: 200 lines → group2 (adding file2 to group1 would be 400 > 300)
	// file3: 200 lines → group3 (adding to group2 would be 400 > 300)
	sections := makeSections(3, 200)
	groups := GroupDiffSections(sections, 5, 300, 10)

	if len(groups) != 3 {
		t.Errorf("GroupDiffSections() returned %d groups, want 3", len(groups))
	}
}

func TestGroupDiffSections_DualThreshold(t *testing.T) {
	// 4 files: two with 100 lines each, two with 200 lines each
	// maxFiles=3, maxLines=250
	// file1(100)+file2(100)=200 → ok, add file3(200)? 200+200=400 > 250 → flush → group1=[f1,f2]
	// file3(200) → group2 start, add file4(200)? 200+200=400>250 → flush → group2=[f3]
	// group3=[f4]
	sections := []DiffSection{
		{FilePath: "f1.go", AddedLines: 100},
		{FilePath: "f2.go", AddedLines: 100},
		{FilePath: "f3.go", AddedLines: 200},
		{FilePath: "f4.go", AddedLines: 200},
	}
	groups := GroupDiffSections(sections, 3, 250, 10)

	if len(groups) != 3 {
		t.Errorf("GroupDiffSections() returned %d groups, want 3", len(groups))
	}
	if len(groups[0].Sections) != 2 {
		t.Errorf("group[0] has %d sections, want 2", len(groups[0].Sections))
	}
	if len(groups[1].Sections) != 1 {
		t.Errorf("group[1] has %d sections, want 1", len(groups[1].Sections))
	}
	if len(groups[2].Sections) != 1 {
		t.Errorf("group[2] has %d sections, want 1", len(groups[2].Sections))
	}
}

func TestGroupDiffSections_MaxGroupsMerge(t *testing.T) {
	// 6 files, each with 10 lines, max 1 file per group → 6 groups → merge to 4
	sections := makeSections(6, 10)
	groups := GroupDiffSections(sections, 1, 300, 4)

	if len(groups) != 4 {
		t.Errorf("GroupDiffSections() returned %d groups, want 4", len(groups))
	}

	// All 6 sections must still be present
	total := 0
	for _, g := range groups {
		total += len(g.Sections)
	}
	if total != 6 {
		t.Errorf("total sections across groups = %d, want 6", total)
	}
}

func TestGroupDiffSections_SingleSection(t *testing.T) {
	sections := []DiffSection{{FilePath: "main.go", AddedLines: 5}}
	groups := GroupDiffSections(sections, 5, 300, 4)

	if len(groups) != 1 {
		t.Fatalf("GroupDiffSections() returned %d groups, want 1", len(groups))
	}
	if groups[0].Key != "g01" {
		t.Errorf("groups[0].Key = %q, want %q", groups[0].Key, "g01")
	}
}

func TestGroupDiffSections_EmptySections(t *testing.T) {
	groups := GroupDiffSections(nil, 5, 300, 4)
	if groups != nil {
		t.Errorf("GroupDiffSections(nil) = %v, want nil", groups)
	}

	groups = GroupDiffSections([]DiffSection{}, 5, 300, 4)
	if groups != nil {
		t.Errorf("GroupDiffSections([]) = %v, want nil", groups)
	}
}

func TestGroupDiffSections_LargeFileExceedsThreshold(t *testing.T) {
	// Single file with 500+ lines exceeds the 300-line threshold but can't be split
	sections := []DiffSection{{FilePath: "large.go", AddedLines: 500}}
	groups := GroupDiffSections(sections, 5, 300, 4)

	if len(groups) != 1 {
		t.Fatalf("GroupDiffSections() returned %d groups, want 1 (can't split a single file)", len(groups))
	}
	if len(groups[0].Sections) != 1 {
		t.Errorf("group[0] has %d sections, want 1", len(groups[0].Sections))
	}
	if groups[0].Sections[0].AddedLines != 500 {
		t.Errorf("section AddedLines = %d, want 500", groups[0].Sections[0].AddedLines)
	}
}

func TestGroupDiffSections_GroupKeys(t *testing.T) {
	sections := makeSections(7, 10)
	// maxFiles=2 → groups: [f1,f2], [f3,f4], [f5,f6], [f7] = 4 groups
	groups := GroupDiffSections(sections, 2, 300, 10)

	expectedKeys := []string{"g01", "g02", "g03", "g04"}
	if len(groups) != len(expectedKeys) {
		t.Fatalf("GroupDiffSections() returned %d groups, want %d", len(groups), len(expectedKeys))
	}
	for i, key := range expectedKeys {
		if groups[i].Key != key {
			t.Errorf("groups[%d].Key = %q, want %q", i, groups[i].Key, key)
		}
	}
}

