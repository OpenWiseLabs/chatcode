package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"chatcode/internal/domain"
)

type Policy struct {
	allowlist map[string]struct{}
	roots     []string
}

func New(allowlist []string, roots []string) *Policy {
	m := make(map[string]struct{}, len(allowlist))
	for _, c := range allowlist {
		m[strings.TrimSpace(c)] = struct{}{}
	}
	cleanRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		cleanRoots = append(cleanRoots, filepath.Clean(r))
	}
	return &Policy{allowlist: m, roots: cleanRoots}
}

func (p *Policy) Validate(job domain.Job) error {
	if err := p.ValidateExecutor(job.Executor); err != nil {
		return err
	}
	if err := p.ValidateWorkdir(job.Workdir); err != nil {
		return err
	}
	return nil
}

func (p *Policy) ValidateExecutor(name string) error {
	if _, ok := p.allowlist[name]; !ok {
		return fmt.Errorf("executor %q is not allowed", name)
	}
	return nil
}

func (p *Policy) ValidateWorkdir(workdir string) error {
	if workdir == "" {
		return errors.New("workdir cannot be empty")
	}
	wd := filepath.Clean(workdir)
	for _, root := range p.roots {
		if wd == root || strings.HasPrefix(wd, root+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("workdir %q is outside allowed roots", wd)
}

func (p *Policy) PrimaryRoot() string {
	if len(p.roots) == 0 {
		return ""
	}
	return p.roots[0]
}
