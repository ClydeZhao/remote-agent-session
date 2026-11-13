package localstate

import (
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"

	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrUnsafe = errors.New("local state requires a private directory and regular owner-only files")

type Store struct{ dir string }

type stateWriteError struct {
	message string
	cause   error
}

func (e *stateWriteError) Error() string {
	return e.message + "（原因：" + stateWriteErrorClass(e.cause) + "）"
}
func (e *stateWriteError) Unwrap() error { return e.cause }

func stateWriteErrorClass(cause error) string {
	for _, classification := range []struct {
		err  error
		code string
	}{
		{syscall.EPERM, "access_denied/EPERM"},
		{syscall.EACCES, "access_denied/EACCES"},
		{syscall.EROFS, "read_only/EROFS"},
		{syscall.ENOSPC, "no_space/ENOSPC"},
		{syscall.EEXIST, "path_conflict/EEXIST"},
		{syscall.EISDIR, "path_conflict/EISDIR"},
		{syscall.ENOTDIR, "path_conflict/ENOTDIR"},
		{syscall.EIO, "io/EIO"},
	} {
		if errors.Is(cause, classification.err) {
			return classification.code
		}
	}
	return "unknown"
}

func stateWriteFailure(message string, cause error) error {
	for {
		switch underlying := cause.(type) {
		case *os.PathError:
			cause = underlying.Err
		case *os.LinkError:
			cause = underlying.Err
		case *os.SyscallError:
			cause = underlying.Err
		default:
			return &stateWriteError{message: message, cause: cause}
		}
	}
}

func Open(dir string) (*Store, error) {
	if err := supportedPlatform(); err != nil {
		return nil, err
	}
	if err := platformfs.EnsureDir(dir); err != nil {
		return nil, errors.New("cannot create local state directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !platformfs.Safe(dir, info, true) {
		return nil, ErrUnsafe
	}
	return &Store{dir: dir}, nil
}

func (s *Store) read(name string, dst any) error {
	p := filepath.Join(s.dir, name)
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !platformfs.Safe(p, info, true) {
		return ErrUnsafe
	}
	f, err := os.Open(p)
	if err != nil {
		return errors.New("cannot read local state")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return errors.New("cannot read local state")
	}
	if json.Unmarshal(data, dst) != nil {
		return errors.New("invalid local state; refusing to replace it")
	}
	return nil
}

func (s *Store) write(name string, value any) error {
	return s.writeWithFileOperations(name, value, platformfs.Replace, os.Remove)
}

func (s *Store) writeWithFileOperations(name string, value any, replace func(string, string) error, remove func(string) error) (resultErr error) {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("cannot encode local state")
	}
	f, err := os.CreateTemp(s.dir, ".write-*")
	if err != nil {
		return errors.New("cannot create private state file")
	}
	replaced := false
	defer func() {
		if replaced {
			return
		}
		if cleanupErr := remove(f.Name()); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, stateWriteFailure("本地状态临时文件清理未完成，本实例可能保留临时文件。请让原 Agent 检查该状态目录的原生权限与清理结果；不要删除或复制登录凭据。", cleanupErr))
		}
	}()
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !platformfs.Safe(f.Name(), info, true) {
		return ErrUnsafe
	}
	if _, err = f.Write(data); err != nil {
		return errors.New("cannot write local state")
	}
	if err = f.Sync(); err != nil {
		return errors.New("cannot sync local state")
	}
	if err = f.Close(); err != nil {
		return errors.New("cannot close local state")
	}
	if err = replace(f.Name(), filepath.Join(s.dir, name)); err != nil {
		return stateWriteFailure("Remote Agent Session 无法原子保存本地状态，本次状态保存未完成。请检查磁盘和状态目录是否可用；若被宿主限制，请在原生权限流程中允许必要的目录写入与重命名，不要放宽文件权限或复制登录凭据。", err)
	}
	replaced = true
	return s.syncDir()
}

func (s *Store) syncDir() error {
	return platformfs.SyncDir(s.dir)
}
