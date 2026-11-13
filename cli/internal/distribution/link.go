package distribution

import (
	"errors"
	"os"
	"path/filepath"
)

func (s Installation) Link(home, agent string) (string, error) {
	dir := ".agents"
	switch agent {
	case "shared", "codex", "deepseek":
	case "claude":
		dir = ".claude"
	default:
		return "", errors.New("choose shared or claude")
	}
	if !s.SkillIntact() {
		return "", errors.New("installed skill is damaged; repair the installation first")
	}
	target := filepath.Join(home, dir, "skills", "remote-agent-session")
	if target == s.SkillDir {
		return target, nil
	}
	if err := safeDir(filepath.Dir(target), true); err != nil {
		return "", err
	}
	if path, err := os.Readlink(target); err == nil && path == s.SkillDir {
		return target, nil
	}
	if err := os.Symlink(s.SkillDir, target); err != nil {
		return "", errors.New("cannot create skill link; check link permissions and existing destination")
	}
	return target, nil
}
