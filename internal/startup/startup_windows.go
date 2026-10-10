//go:build windows && (amd64 || arm64)

// Package startup 管理当前用户的 Run 项；无需管理员权限。
package startup

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

var (
	advapi    = winapi.DLL("advapi32.dll")
	regOpen   = advapi.NewProc("RegOpenKeyExW")
	regCreate = advapi.NewProc("RegCreateKeyExW")
	regQuery  = advapi.NewProc("RegQueryValueExW")
	regSet    = advapi.NewProc("RegSetValueExW")
	regDelete = advapi.NewProc("RegDeleteValueW")
	regClose  = advapi.NewProc("RegCloseKey")
)

const runPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func Enabled() (bool, error) {
	var h uintptr
	path, name := winapi.UTF16(runPath), winapi.UTF16("GeoPulse")
	code, _, _ := regOpen.Call(^uintptr(0x7ffffffe), uintptr(unsafe.Pointer(path)), 0, 0x20019, uintptr(unsafe.Pointer(&h)))
	if code == 2 {
		return false, nil
	}
	if code != 0 {
		return false, syscall.Errno(code)
	}
	defer regClose.Call(h)
	var data [4096]uint16
	size, kind := uint32(unsafe.Sizeof(data)), uint32(0)
	code, _, _ = regQuery.Call(h, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)))
	if code == 2 {
		return false, nil
	}
	if code != 0 {
		return false, syscall.Errno(code)
	}
	if kind != 1 {
		return false, fmt.Errorf("GeoPulse 自启项不是 REG_SZ")
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(syscall.UTF16ToString(data[:]), `"`+exe+`"`), nil
}

func Set(enabled bool) error {
	var h uintptr
	path, name := winapi.UTF16(runPath), winapi.UTF16("GeoPulse")
	code, _, _ := regCreate.Call(^uintptr(0x7ffffffe), uintptr(unsafe.Pointer(path)), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&h)), 0)
	if code != 0 {
		return syscall.Errno(code)
	}
	defer regClose.Call(h)
	if !enabled {
		code, _, _ = regDelete.Call(h, uintptr(unsafe.Pointer(name)))
		if code == 2 {
			return nil
		}
	} else {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		data, err := syscall.UTF16FromString(`"` + exe + `"`)
		if err != nil {
			return err
		}
		code, _, _ = regSet.Call(h, uintptr(unsafe.Pointer(name)), 0, 1, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	}
	if code != 0 {
		return syscall.Errno(code)
	}
	return nil
}
