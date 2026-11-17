package relay

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validateControlSocket(path string) error {
	_, release, err := prepareControlSocket(path)
	if err == nil {
		release()
	}
	return err
}

func prepareControlSocket(path string) (string, func(), error) {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.Contains(path[len(filepath.VolumeName(path)):], ":") {
		return "", nil, ErrInterruptUnavailable
	}
	path = filepath.Clean(path)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", nil, ErrInterruptUnavailable
	}
	handles := []windows.Handle{}
	release := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			windows.CloseHandle(handles[i])
		}
	}
	fail := func() (string, func(), error) {
		release()
		return "", nil, ErrInterruptUnavailable
	}
	var parents []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		parents = append(parents, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(parents) - 1; i >= 0; i-- {
		name, err := windows.UTF16PtrFromString(parents[i])
		if err != nil {
			return fail()
		}
		handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return fail()
		}
		handles = append(handles, handle)
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fail()
		}
		sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil || !controlSocketSecuritySafe(sd, user.User.Sid, i == 0) {
			return fail()
		}
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return fail()
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || !controlSocketSecuritySafe(sd, user.User.Sid, true) {
		return fail()
	}
	return path, release, nil
}

func controlSocketSecuritySafe(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID, private bool) bool {
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	for i := uint16(0); i < acl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, uint32(i), &ace) != nil {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		if trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			continue
		}
		const replaceRights = 0x40 | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_ALL | windows.GENERIC_WRITE
		if private || ace.Mask&replaceRights != 0 {
			return false
		}
	}
	return true
}
