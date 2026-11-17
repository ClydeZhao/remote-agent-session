import csv
import ctypes
import stat
import subprocess
from ctypes import wintypes
from functools import lru_cache
from pathlib import Path


@lru_cache(maxsize=1)
def current_sid():
    result = subprocess.check_output(["whoami", "/user", "/fo", "csv", "/nh"], text=True)
    sid = next(csv.reader([result.strip()]))[1]
    if not sid.startswith("S-1-5-"):
        raise RuntimeError("Cannot establish current Windows user SID")
    return sid


def security_api():
    api = ctypes.WinDLL("advapi32", use_last_error=True)
    pointer = ctypes.c_void_p
    out_pointer = ctypes.POINTER(pointer)
    out_bool = ctypes.POINTER(wintypes.BOOL)
    api.ConvertStringSecurityDescriptorToSecurityDescriptorW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, out_pointer, ctypes.POINTER(wintypes.DWORD)]
    api.ConvertStringSidToSidW.argtypes = [wintypes.LPCWSTR, out_pointer]
    api.SetFileSecurityW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, pointer]
    api.GetFileSecurityW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, pointer, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
    api.GetSecurityDescriptorOwner.argtypes = [pointer, out_pointer, out_bool]
    api.GetSecurityDescriptorDacl.argtypes = [pointer, out_bool, out_pointer, out_bool]
    api.GetSecurityDescriptorControl.argtypes = [pointer, ctypes.POINTER(wintypes.WORD), ctypes.POINTER(wintypes.DWORD)]
    api.GetAclInformation.argtypes = [pointer, pointer, wintypes.DWORD, ctypes.c_int]
    api.GetAce.argtypes = [pointer, wintypes.DWORD, out_pointer]
    api.EqualSid.argtypes = [pointer, pointer]
    return api


def free_local(pointer):
    api = ctypes.WinDLL("kernel32", use_last_error=True)
    api.LocalFree.argtypes = [ctypes.c_void_p]
    api.LocalFree.restype = ctypes.c_void_p
    api.LocalFree(pointer)


def private_acl(api, descriptor, sid, directory):
    class AclSize(ctypes.Structure):
        _fields_ = [("AceCount", wintypes.DWORD), ("AclBytesInUse", wintypes.DWORD), ("AclBytesFree", wintypes.DWORD)]
    owner = ctypes.c_void_p()
    defaulted = wintypes.BOOL()
    present = wintypes.BOOL()
    dacl = ctypes.c_void_p()
    control = wintypes.WORD()
    revision = wintypes.DWORD()
    if not api.GetSecurityDescriptorOwner(descriptor, ctypes.byref(owner), ctypes.byref(defaulted)) or not owner or not api.EqualSid(owner, sid):
        return False
    if not api.GetSecurityDescriptorDacl(descriptor, ctypes.byref(present), ctypes.byref(dacl), ctypes.byref(defaulted)) or not present.value or not dacl:
        return False
    if not api.GetSecurityDescriptorControl(descriptor, ctypes.byref(control), ctypes.byref(revision)):
        raise ctypes.WinError(ctypes.get_last_error())
    size = AclSize()
    if not api.GetAclInformation(dacl, ctypes.byref(size), ctypes.sizeof(size), 2):
        raise ctypes.WinError(ctypes.get_last_error())
    if size.AceCount != 1:
        return False
    ace = ctypes.c_void_p()
    if not api.GetAce(dacl, 0, ctypes.byref(ace)):
        raise ctypes.WinError(ctypes.get_last_error())
    ace_type = ctypes.c_ubyte.from_address(ace.value).value
    ace_flags = ctypes.c_ubyte.from_address(ace.value + 1).value
    ace_mask = wintypes.DWORD.from_address(ace.value + 4).value
    if ace_type != 0 or ace_mask != 0x1F01FF or not api.EqualSid(ctypes.c_void_p(ace.value + 8), sid):
        return False
    protected = bool(control.value & 0x1000)
    return protected and ace_flags == (3 if directory else 0)


def secure_path(path, create=False):
    path = Path(path)
    info = path.lstat()
    if info.st_file_attributes & stat.FILE_ATTRIBUTE_REPARSE_POINT:
        raise RuntimeError("Private Windows paths cannot be reparse points")
    api = security_api()
    sid_text = current_sid()
    directory = path.is_dir()
    flags = "OICI" if directory else ""
    if create:
        descriptor = ctypes.c_void_p()
        if not api.ConvertStringSecurityDescriptorToSecurityDescriptorW("O:" + sid_text + "D:P(A;" + flags + ";FA;;;" + sid_text + ")", 1, ctypes.byref(descriptor), None):
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            if not api.SetFileSecurityW(str(path), 0x80000004, descriptor):
                raise ctypes.WinError(ctypes.get_last_error())
        finally:
            free_local(descriptor)
    size = wintypes.DWORD()
    api.GetFileSecurityW(str(path), 5, None, 0, ctypes.byref(size))
    buffer = ctypes.create_string_buffer(size.value)
    if not api.GetFileSecurityW(str(path), 5, buffer, size, ctypes.byref(size)):
        raise ctypes.WinError(ctypes.get_last_error())
    sid = ctypes.c_void_p()
    if not api.ConvertStringSidToSidW(sid_text, ctypes.byref(sid)):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        protected = private_acl(api, buffer, sid, directory)
    finally:
        free_local(sid)
    if protected:
        return path
    raise RuntimeError("Private Windows path must have a protected current-user-only ACL: " + str(path))

