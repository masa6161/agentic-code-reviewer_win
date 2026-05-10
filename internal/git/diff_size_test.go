package git

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestParseDiffStat(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantFiles int
		wantLines int
	}{
		{
			name:      "typical output",
			output:    " 3 files changed, 45 insertions(+), 12 deletions(-)\n",
			wantFiles: 3,
			wantLines: 57,
		},
		{
			name:      "insertions only",
			output:    " 1 file changed, 10 insertions(+)\n",
			wantFiles: 1,
			wantLines: 10,
		},
		{
			name:      "deletions only",
			output:    " 2 files changed, 8 deletions(-)\n",
			wantFiles: 2,
			wantLines: 8,
		},
		{
			name:      "with file list",
			output:    " foo.go | 10 ++++\n bar.go | 5 ---\n 2 files changed, 10 insertions(+), 5 deletions(-)\n",
			wantFiles: 2,
			wantLines: 15,
		},
		{
			name:      "empty output",
			output:    "",
			wantFiles: 0,
			wantLines: 0,
		},
		{
			name:      "single file singular",
			output:    " 1 file changed, 1 insertion(+), 1 deletion(-)\n",
			wantFiles: 1,
			wantLines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, lines := parseDiffStat(tt.output)
			if files != tt.wantFiles {
				t.Errorf("parseDiffStat() files = %d, want %d", files, tt.wantFiles)
			}
			if lines != tt.wantLines {
				t.Errorf("parseDiffStat() lines = %d, want %d", lines, tt.wantLines)
			}
		})
	}
}

func TestClassifySize(t *testing.T) {
	tests := []struct {
		name      string
		fileCount int
		lineCount int
		want      DiffSize
	}{
		{"small - few files few lines", 2, 50, DiffSizeSmall},
		{"small - boundary", 3, 100, DiffSizeSmall},
		{"medium - files threshold", 4, 50, DiffSizeMedium},
		{"medium - lines threshold", 2, 101, DiffSizeMedium},
		{"medium - both medium", 5, 200, DiffSizeMedium},
		{"medium - single file many lines", 1, 501, DiffSizeMedium},
		{"medium - single file huge", 1, 2000, DiffSizeMedium},
		{"large - many files", 11, 50, DiffSizeLarge},
		{"large - many lines", 2, 501, DiffSizeLarge},
		{"medium - boundary 2 files 500 lines", 2, 500, DiffSizeMedium},
		{"large - both large", 15, 1000, DiffSizeLarge},
		{"zero values", 0, 0, DiffSizeSmall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySize(tt.fileCount, tt.lineCount)
			if got != tt.want {
				t.Errorf("classifySize(%d, %d) = %v, want %v", tt.fileCount, tt.lineCount, got, tt.want)
			}
		})
	}
}

func TestDiffSize_String(t *testing.T) {
	tests := []struct {
		size DiffSize
		want string
	}{
		{DiffSizeSmall, domain.SizeSmall},
		{DiffSizeMedium, domain.SizeMedium},
		{DiffSizeLarge, domain.SizeLarge},
		{DiffSize(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.size.String(); got != tt.want {
				t.Errorf("DiffSize(%d).String() = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}
