//go:build darwin || linux

package localstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAtomicStateReplacementPreservesFailureClassWithoutPaths(t *testing.T) {
	s := newStore(t)
	destination := filepath.Join(s.dir, "private-state.json")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	err := s.write("private-state.json", map[string]string{"token": "synthetic-secret"})
	if !errors.Is(err, syscall.EEXIST) {
		t.Fatalf("rename failure lost its filesystem classification: %v", err)
	}
	for _, content := range []string{s.dir, "private-state.json", ".write-", "synthetic-secret"} {
		if strings.Contains(err.Error(), content) {
			t.Fatal("state replacement error exposes private operation data")
		}
	}
	if !strings.Contains(err.Error(), "原生权限") {
		t.Fatal("state replacement error offers no supported permission guidance")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "private-state.json" || !entries[0].IsDir() {
		t.Fatal("replacement failure changed target or left a removable temporary file", err)
	}
}

func TestAtomicStateReplacementReportsPrimaryAndCleanupFailures(t *testing.T) {
	s := newStore(t)
	var temporary string
	err := s.writeWithFileOperations("state.json", map[string]string{"token": "synthetic-secret"}, func(source, target string) error {
		temporary = source
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("destination changed before replacement")
		}
		return &os.LinkError{Op: "rename", Old: source, New: target, Err: syscall.EPERM}
	}, func(path string) error {
		if path != temporary {
			t.Fatal("cleanup targeted a different path")
		}
		return &os.PathError{Op: "remove", Path: path, Err: syscall.EACCES}
	})
	if !errors.Is(err, syscall.EPERM) || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("primary or cleanup failure lost its classification: %v", err)
	}
	if !strings.Contains(err.Error(), "本实例可能保留临时文件") {
		t.Fatal("cleanup failure was not disclosed")
	}
	for _, content := range []string{s.dir, "state.json", ".write-", "synthetic-secret"} {
		if strings.Contains(err.Error(), content) {
			t.Fatal("combined state error exposes private operation data")
		}
	}
	var pathError *os.PathError
	var linkError *os.LinkError
	if errors.As(err, &pathError) || errors.As(err, &linkError) {
		t.Fatal("private paths remain exposed through wrapped filesystem errors")
	}
	if _, err := os.Stat(temporary); err != nil {
		t.Fatal("failed cleanup unexpectedly removed the temporary file")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed replacement unexpectedly changed destination")
	}
}

func TestSuccessfulAtomicStateReplacementDoesNotRemoveCommittedFile(t *testing.T) {
	s := newStore(t)
	cleaned := false
	err := s.writeWithFileOperations("state.json", "saved", os.Rename, func(path string) error {
		cleaned = true
		return &os.PathError{Op: "remove", Path: path, Err: syscall.EACCES}
	})
	if err != nil || cleaned {
		t.Fatalf("committed replacement performed unnecessary cleanup: %v", err)
	}
	var actual string
	if err := s.read("state.json", &actual); err != nil || actual != "saved" {
		t.Fatal("replacement did not preserve committed content", err)
	}
}

func TestStateWriteErrorDisplaysSafeFilesystemClassification(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		code  string
	}{
		{"operation denied", syscall.EPERM, "access_denied/EPERM"},
		{"access denied", syscall.EACCES, "access_denied/EACCES"},
		{"read only", syscall.EROFS, "read_only/EROFS"},
		{"no space", syscall.ENOSPC, "no_space/ENOSPC"},
		{"existing target", syscall.EEXIST, "path_conflict/EEXIST"},
		{"directory target", syscall.EISDIR, "path_conflict/EISDIR"},
		{"invalid parent", syscall.ENOTDIR, "path_conflict/ENOTDIR"},
		{"io failure", syscall.EIO, "io/EIO"},
		{"unknown", errors.New("private-secret-error"), "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := stateWriteFailure("固定操作指引", &os.LinkError{Op: "rename", Old: "/private/.write-random", New: "/private/credential.json", Err: test.cause})
			if !strings.Contains(err.Error(), "原因："+test.code) || !strings.Contains(err.Error(), "固定操作指引") {
				t.Fatalf("CLI-visible text lacks filesystem classification: %v", err)
			}
			if !errors.Is(err, test.cause) {
				t.Fatal("classification changed the underlying cause")
			}
			for _, sensitive := range []string{"/private", ".write-random", "credential.json", "private-secret-error"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatal("filesystem classification exposes private details")
				}
			}
		})
	}
}
