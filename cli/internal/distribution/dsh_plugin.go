package distribution

import (
	"encoding/json"
	"errors"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo"
	"os"
	"path/filepath"
)

func PrepareDSHPlugin(executable string) (string, error) {
	if buildinfo.DSHPluginSHA256 != "" {
		archive, err := PackagedDSHPlugin(executable)
		if err != nil {
			return "", err
		}
		root := filepath.Join(filepath.Dir(filepath.Dir(executable)), "share", "remote-agent-session", "plugins")
		if err := safeDir(root, true); err != nil {
			return "", err
		}
		destination := filepath.Join(root, buildinfo.DSHPluginSHA256+".tgz")
		if regular(destination) == nil {
			data, err := os.ReadFile(destination)
			if err == nil && Digest(data) == buildinfo.DSHPluginSHA256 {
				return destination, nil
			}
			return "", errors.New("插件缓存内容与发行摘要不符，请重新安装完整发行版")
		} else if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			return "", errors.New("插件缓存路径不安全，未覆盖")
		}
		data, err := os.ReadFile(archive)
		if err != nil || Digest(data) != buildinfo.DSHPluginSHA256 {
			return "", errors.New("插件资源在准备期间发生变化，未安装")
		}
		if err := atomic(destination, data, 0600); err != nil {
			return "", err
		}
		return destination, nil
	}
	s, err := Discover(executable)
	if err != nil {
		return "", err
	}
	unlock, err := lock(s.root())
	if err != nil {
		return "", err
	}
	defer unlock()
	s, err = Discover(executable)
	if err != nil {
		return "", err
	}
	if s.Pending() || !s.Intact() {
		return "", errors.New("Remote Agent Session 安装尚未完整，请重新安装完整发行版修复，再安装插件")
	}
	if s.DSHPlugin == (Artifact{}) && buildinfo.DSHPluginSHA256 != "" {
		manifest, err := Check(s.Source, s.AllowLoopback)
		if err != nil {
			return "", err
		}
		s, err = s.restoreDSHPluginRecord(manifest)
		if err != nil {
			return "", err
		}
	}
	a := s.DSHPlugin
	if !validDigest(a.SHA256) || ValidateURL(a.URL, s.AllowLoopback) != nil || !sameOrigin(s.Source, a.URL) {
		return "", errors.New("当前安装没有匹配的 DeepSeek Harness 插件，请先更新 Remote Agent Session")
	}
	dir := filepath.Join(s.root(), "plugins")
	if err := safeDir(dir, true); err != nil {
		return "", err
	}
	path := filepath.Join(dir, a.SHA256+".tgz")
	if _, err := os.Lstat(path); err == nil {
		if regular(path) != nil {
			return "", errors.New("unsafe cached plugin archive")
		}
		data, err := os.ReadFile(path)
		if err == nil && Digest(data) == a.SHA256 {
			return path, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, err := resource(s.Source, a, s.AllowLoopback, 2<<20)
	if err != nil {
		return "", err
	}
	if err := atomic(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func (s Installation) restoreDSHPluginRecord(m Manifest) (Installation, error) {
	if s.DSHPlugin != (Artifact{}) || buildinfo.DSHPluginSHA256 == "" {
		return s, nil
	}
	if s.Version != buildinfo.Version || s.Revision != buildinfo.Revision || m.Version != s.Version || m.Revision != s.Revision || m.Artifacts[Platform()].SHA256 != s.BinaryHash || m.Artifacts["skill"].SHA256 != s.SkillHash || m.Artifacts["deepseek-harness"].SHA256 != buildinfo.DSHPluginSHA256 {
		return s, errors.New("无法确认旧安装记录对应的插件版本，请使用匹配版本的固定安装器修复")
	}
	s.DSHPlugin = m.Artifacts["deepseek-harness"]
	data, err := json.Marshal(s)
	if err == nil {
		err = atomic(filepath.Join(s.root(), "install.json"), data, 0600)
	}
	return s, err
}

func PackagedDSHPlugin(executable string) (string, error) {
	if buildinfo.DSHPluginSHA256 == "" {
		return "", errors.New("没有匹配插件摘要")
	}
	root := filepath.Dir(filepath.Dir(executable))
	archive := filepath.Join(root, "share", "remote-agent-session", "deepseek-harness.tgz")
	if err := regular(archive); err != nil {
		return "", err
	}
	data, err := os.ReadFile(archive)
	if err != nil || len(data) > 2<<20 || Digest(data) != buildinfo.DSHPluginSHA256 {
		return "", errors.New("匹配插件资源校验失败，请重新安装完整发行版")
	}
	return archive, nil
}
