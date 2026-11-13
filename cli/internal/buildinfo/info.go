package buildinfo

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

var Version = "0.1.0-dev"
var Revision = "unknown"
var DSHPluginSHA256 = ""

const APIVersion = "0.1.0"

//go:embed SKILL.md references/*.md
var resources embed.FS

func Skill() []byte { data, _ := resources.ReadFile("SKILL.md"); return data }
func Info() map[string]string {
	sum := sha256.Sum256(Skill())
	return map[string]string{"skill_sha256": hex.EncodeToString(sum[:]), "skill_references_sha256": ReferenceDigest(SkillFiles()), "version": Version, "revision": Revision, "api_version": APIVersion, "skill_version": Version, "dsh_plugin_sha256": DSHPluginSHA256}
}

func SkillFiles() map[string]string {
	files := map[string]string{}
	fs.WalkDir(resources, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, _ := resources.ReadFile(path)
			files[path] = string(data)
		}
		return nil
	})
	return files
}
func ReadSkill(topic string) ([]byte, error) {
	if topic == "" || topic == "SKILL.md" {
		return Skill(), nil
	}
	for name, data := range SkillFiles() {
		if name == "references/"+topic+".md" || name == topic {
			return []byte(data), nil
		}
	}
	return nil, fmt.Errorf("unknown skill reference: %s", topic)
}
func ReferenceDigest(files map[string]string) string {
	names := []string{}
	for name := range files {
		if name != "SKILL.md" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(files[name]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
