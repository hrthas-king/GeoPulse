//go:build windows && (amd64 || arm64)

// Package winapi 提供少量系统 DLL 辅助函数。只支持 64 位 ABI。
package winapi

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DLL 必须从系统目录加载，避免当前目录中同名 DLL 被误加载。
func DLL(name string) *syscall.LazyDLL {
	return syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", name))
}

func UTF16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

func Error(api string, err error) error {
	if err == syscall.Errno(0) {
		return fmt.Errorf("%s 失败", api)
	}
	return fmt.Errorf("%s: %w", api, err)
}
