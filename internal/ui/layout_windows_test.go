//go:build windows && (amd64 || arm64)

package ui

import (
	"testing"
	"unsafe"
)

func TestNativeUILayout(t *testing.T) {
	var ni notifyIcon
	if unsafe.Sizeof(ni) != 976 || unsafe.Offsetof(ni.Icon) != 32 || unsafe.Offsetof(ni.Tip) != 40 || unsafe.Offsetof(ni.BalloonIcon) != 968 {
		t.Fatalf("NOTIFYICONDATAW ABI 错误: size=%d icon=%d tip=%d balloon=%d", unsafe.Sizeof(ni), unsafe.Offsetof(ni.Icon), unsafe.Offsetof(ni.Tip), unsafe.Offsetof(ni.BalloonIcon))
	}
	if unsafe.Sizeof(windowClass{}) != 80 || unsafe.Sizeof(message{}) != 48 {
		t.Fatal("窗口消息 ABI 错误")
	}
}
