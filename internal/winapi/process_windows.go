//go:build windows && (amd64 || arm64)

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel            = DLL("kernel32.dll")
	createMutex       = kernel.NewProc("CreateMutexW")
	closeKernelHandle = kernel.NewProc("CloseHandle")
	processMemory     = kernel.NewProc("K32GetProcessMemoryInfo")
	guiResources      = DLL("user32.dll").NewProc("GetGuiResources")
)

func SingleInstance() (release func(), alreadyRunning bool, err error) {
	name := UTF16(`Local\GeoPulse.Singleton`)
	h, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, false, Error("CreateMutexW", callErr)
	}
	release = func() { closeKernelHandle.Call(h) }
	return release, callErr == syscall.Errno(183), nil
}

// RequestExit 供更新时的 --quit 使用，向同一会话的 GeoPulse 隐藏窗口请求正常退出。
func RequestExit() error {
	user := DLL("user32.dll")
	name := UTF16("GeoPulseHiddenWindow")
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(name)), 0)
	if hwnd == 0 {
		return nil
	}
	if ok, _, err := user.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0); ok == 0 {
		return Error("GeoPulse exit", err)
	}
	return nil
}

type Memory struct {
	PrivateWorkingSetBytes *uint64 `json:"private_working_set_bytes,omitempty"`
	WorkingSetBytes        uint64  `json:"working_set_bytes"`
	PeakWorkingSetBytes    uint64  `json:"peak_working_set_bytes"`
	PrivateBytes           uint64  `json:"private_bytes"`
	GDIObjects             uint32  `json:"gdi_objects"`
	USERObjects            uint32  `json:"user_objects"`
}

func ProcessMemory() (Memory, error) {
	var counters struct {
		Size, PageFaultCount                                                                                  uint32
		PeakWorkingSet, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
		PrivateWorkingSet                                                                                     uintptr
		SharedCommit                                                                                          uint64
	}
	counters.Size = uint32(unsafe.Sizeof(counters))
	ok, _, err := processMemory.Call(^uintptr(0), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		// EX2 需要较新 Windows；旧版本仍可读取 80 字节 EX 结构。
		counters.Size = 80
		ok, _, err = processMemory.Call(^uintptr(0), uintptr(unsafe.Pointer(&counters)), 80)
	}
	if ok == 0 {
		return Memory{}, fmt.Errorf("读取内存: %w", err)
	}
	gdi, _, _ := guiResources.Call(^uintptr(0), 0)
	user, _, _ := guiResources.Call(^uintptr(0), 1)
	var privateWorkingSet *uint64
	if counters.PrivateWorkingSet > 0 {
		v := uint64(counters.PrivateWorkingSet)
		privateWorkingSet = &v
	}
	return Memory{PrivateWorkingSetBytes: privateWorkingSet, WorkingSetBytes: uint64(counters.WorkingSet), PeakWorkingSetBytes: uint64(counters.PeakWorkingSet),
		PrivateBytes: uint64(counters.Private), GDIObjects: uint32(gdi), USERObjects: uint32(user)}, nil
}
