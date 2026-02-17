package security

import (
	"testing"

	"chatcode/internal/domain"
)

func TestPolicyValidate(t *testing.T) {
	p := New([]string{"codex", "claude"}, []string{"/repo"})
	job := domain.Job{Executor: "codex", Workdir: "/repo/app"}
	if err := p.Validate(job); err != nil {
		t.Fatalf("expected valid job, got %v", err)
	}
}

func TestPolicyRejectsOutsideRoot(t *testing.T) {
	p := New([]string{"codex"}, []string{"/repo"})
	job := domain.Job{Executor: "codex", Workdir: "/etc"}
	if err := p.Validate(job); err == nil {
		t.Fatalf("expected error for outside root")
	}
}
