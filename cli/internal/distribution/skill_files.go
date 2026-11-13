package distribution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo"
)

var referenceName = regexp.MustCompile(`^references/[a-z][a-z0-9_-]{0,63}\.md$`)

func referenceHashes(raw string) (map[string]string, error) {
	result := map[string]string{}
	if raw == "" {
		return result, nil
	}
	if len(raw) > 16384 || json.Unmarshal([]byte(raw), &result) != nil || len(result) > 32 {
		return nil, errors.New("invalid skill reference metadata")
	}
	for name, hash := range result {
		if !referenceName.MatchString(name) || !validDigest(hash) {
			return nil, errors.New("invalid skill reference metadata")
		}
	}
	return result, nil
}
func (s Installation) referencesIntact() bool {
	hashes, err := referenceHashes(s.References)
	if err != nil {
		return false
	}
	if s.Version == buildinfo.Version && s.Revision == buildinfo.Revision {
		expected := buildinfo.SkillFiles()
		if len(hashes) != len(expected)-1 {
			return false
		}
		for name, data := range expected {
			if name != "SKILL.md" && hashes[name] != Digest([]byte(data)) {
				return false
			}
		}
	}
	if len(hashes) > 0 && safeDir(filepath.Join(s.SkillDir, "references"), false) != nil {
		return false
	}
	for name, hash := range hashes {
		path := filepath.Join(s.SkillDir, filepath.FromSlash(name))
		if regular(path) != nil {
			return false
		}
		data, err := os.ReadFile(path)
		if err != nil || Digest(data) != hash {
			return false
		}
	}
	return true
}
func candidateReferences(path string, info map[string]string, skill []byte) (map[string]string, error) {
	if info["skill_references_sha256"] == "" {
		return map[string]string{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--json", "skills", "export").Output()
	var response struct {
		Data struct {
			Files map[string]string `json:"files"`
		} `json:"data"`
	}
	if err != nil || len(out) > 1<<20 || json.Unmarshal(out, &response) != nil {
		return nil, errors.New("invalid candidate skill bundle")
	}
	files := response.Data.Files
	if len(files) < 1 || len(files) > 33 || files["SKILL.md"] != string(skill) || buildinfo.ReferenceDigest(files) != info["skill_references_sha256"] {
		return nil, errors.New("candidate skill bundle mismatch")
	}
	delete(files, "SKILL.md")
	for name := range files {
		if !referenceName.MatchString(name) {
			return nil, errors.New("invalid candidate reference path")
		}
	}
	return files, nil
}
func (s Installation) referencePaths(names []string) ([]string, error) {
	if len(names) > 32 {
		return nil, errors.New("too many skill references")
	}
	paths := append([]string{}, s.paths()...)
	previous := ""
	for _, name := range names {
		if !referenceName.MatchString(name) || name <= previous {
			return nil, errors.New("invalid recovery reference path")
		}
		previous = name
		paths = append(paths, filepath.Join(s.SkillDir, filepath.FromSlash(name)))
	}
	if len(names) > 0 {
		if err := safeDir(filepath.Join(s.SkillDir, "references"), true); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
func mergedReferenceNames(old map[string]string, next map[string]string) []string {
	set := map[string]bool{}
	for n := range old {
		set[n] = true
	}
	for n := range next {
		set[n] = true
	}
	names := []string{}
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
