package agent

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestResolvePrompts_RolePromptsEnabled_ArchPhase(t *testing.T) {
	config := &ReviewConfig{Phase: domain.PhaseArch, RolePrompts: true, HasArchReviewer: true}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != AutoPhaseArchPrompt {
		t.Errorf("DefaultPrompt = %q, want AutoPhaseArchPrompt", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != AutoPhaseArchRefFilePrompt {
		t.Errorf("RefFilePrompt = %q, want AutoPhaseArchRefFilePrompt", dc.RefFilePrompt)
	}
}

func TestResolvePrompts_RolePromptsEnabled_DiffPhase(t *testing.T) {
	// arch reviewer が存在する場合のみ AutoPhaseDiffPrompt に切替
	config := &ReviewConfig{Phase: domain.PhaseDiff, RolePrompts: true, HasArchReviewer: true}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != AutoPhaseDiffPrompt {
		t.Errorf("DefaultPrompt = %q, want AutoPhaseDiffPrompt", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != AutoPhaseDiffRefFilePrompt {
		t.Errorf("RefFilePrompt = %q, want AutoPhaseDiffRefFilePrompt", dc.RefFilePrompt)
	}
}

func TestResolvePrompts_RolePromptsEnabled_DiffPhase_NoArch(t *testing.T) {
	// arch reviewer なし → RolePrompts を適用しない (レガシーのまま)
	config := &ReviewConfig{Phase: domain.PhaseDiff, RolePrompts: true, HasArchReviewer: false}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != DefaultClaudePrompt {
		t.Errorf("DefaultPrompt should be unchanged when no arch reviewer: %q", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != DefaultClaudeRefFilePrompt {
		t.Errorf("RefFilePrompt should be unchanged when no arch reviewer: %q", dc.RefFilePrompt)
	}
}

func TestResolvePrompts_RolePromptsEnabled_NoPhase(t *testing.T) {
	config := &ReviewConfig{Phase: "", RolePrompts: true}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != DefaultClaudePrompt {
		t.Errorf("DefaultPrompt changed for flat review: %q", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != DefaultClaudeRefFilePrompt {
		t.Errorf("RefFilePrompt changed for flat review: %q", dc.RefFilePrompt)
	}
}

func TestResolvePrompts_RolePromptsDisabled_ArchPhase(t *testing.T) {
	config := &ReviewConfig{Phase: domain.PhaseArch, RolePrompts: false}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != DefaultArchPrompt {
		t.Errorf("DefaultPrompt = %q, want DefaultArchPrompt (legacy)", dc.DefaultPrompt)
	}
	// guards legacy behavior: RolePrompts=false 時は arch RefFile を上書きしない (auto-phase 経路でのみ修正)
	if dc.RefFilePrompt != DefaultClaudeRefFilePrompt {
		t.Errorf("RefFilePrompt should be unchanged in legacy path")
	}
}

func TestResolvePrompts_RolePromptsDisabled_DiffPhase(t *testing.T) {
	config := &ReviewConfig{Phase: domain.PhaseDiff, RolePrompts: false}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != DefaultClaudePrompt {
		t.Errorf("DefaultPrompt should be unchanged: %q", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != DefaultClaudeRefFilePrompt {
		t.Errorf("RefFilePrompt should be unchanged: %q", dc.RefFilePrompt)
	}
}

func TestResolvePrompts_RolePromptsDisabled_NoPhase(t *testing.T) {
	config := &ReviewConfig{Phase: "", RolePrompts: false}
	dc := diffReviewConfig{
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	}
	resolvePrompts(config, &dc)
	if dc.DefaultPrompt != DefaultClaudePrompt {
		t.Errorf("DefaultPrompt should be unchanged: %q", dc.DefaultPrompt)
	}
	if dc.RefFilePrompt != DefaultClaudeRefFilePrompt {
		t.Errorf("RefFilePrompt should be unchanged: %q", dc.RefFilePrompt)
	}
}
