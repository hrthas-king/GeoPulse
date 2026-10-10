//go:build windows && (amd64 || arm64)

package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/app"
	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

// 用真实 HICON -> GDI 像素回读验证图标朝向，覆盖托盘的实际创建路径。
// 不仅检查 Pixels 的源数组，避免再次漏掉 Win32 位图行序问题。
func TestNativeTrayIconIsUpright(t *testing.T) {
	tray := NewTray()
	tray.instance, _, _ = getModule.Call(0)
	snapshot := app.Snapshot{Status: monitor.Online, Geo: ipinfo.Info{Country: "TH"}}
	// 未创建窗口时 Shell 通知预期失败，但 update 已生成实际托盘图标。
	_ = tray.update(snapshot)
	if tray.data.Icon == 0 {
		t.Fatal("native icon was not created")
	}
	defer func() {
		for _, icon := range tray.icons {
			destroyIcon.Call(icon)
		}
	}()
	var info struct {
		Icon               uint32
		HotspotX, HotspotY uint32
		_                  uint32
		Mask, Color        uintptr
	}
	getIconInfo := user32.NewProc("GetIconInfo")
	if ok, _, err := getIconInfo.Call(tray.data.Icon, uintptr(unsafe.Pointer(&info))); ok == 0 {
		t.Fatal(err)
	}
	gdi := winapi.DLL("gdi32.dll")
	deleteObject := gdi.NewProc("DeleteObject")
	defer deleteObject.Call(info.Mask)
	defer deleteObject.Call(info.Color)
	var bitmap struct {
		Size                        uint32
		Width, Height               int32
		Planes, BitCount            uint16
		Compression, SizeImage      uint32
		XPels, YPels                int32
		ColorsUsed, ColorsImportant uint32
		Color                       uint32
	}
	bitmap.Size = 40
	bitmap.Width = 32
	bitmap.Height = -32
	bitmap.Planes = 1
	bitmap.BitCount = 32
	var pixels [4096]byte
	dc, _, _ := user32.NewProc("GetDC").Call(0)
	if dc == 0 {
		t.Fatal("GetDC failed")
	}
	defer user32.NewProc("ReleaseDC").Call(0, dc)
	rows, _, err := gdi.NewProc("GetDIBits").Call(dc, info.Color, 0, 32, uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bitmap)), 0)
	if rows != 32 {
		t.Fatalf("GetDIBits rows=%d err=%v", rows, err)
	}
	i := (25*32 + 16) * 4
	if pixels[i] != 94 || pixels[i+1] != 197 || pixels[i+2] != 34 {
		t.Fatalf("status dot should be below country letters; pixel at (16,25) is BGR(%d,%d,%d), want (94,197,34)", pixels[i], pixels[i+1], pixels[i+2])
	}
	want := Pixels("TH", monitor.Online, false, false, true)
	for y := 4; y < 18; y++ {
		for x := 5; x < 27; x++ {
			i := (y*32 + x) * 4
			if pixels[i] != want[i+2] || pixels[i+1] != want[i+1] || pixels[i+2] != want[i] {
				t.Fatalf("country letters vertically inverted at (%d,%d)", x, y)
			}
		}
	}
	// 可选导出真实 HICON 回读图，方便核对字母和上下行箭头朝向。
	if path := os.Getenv("GEOPULSE_ICON_PREVIEW"); path != "" {
		preview := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				i := ((y/8)*32 + x/8) * 4
				preview.SetNRGBA(x, y, color.NRGBA{pixels[i+2], pixels[i+1], pixels[i], want[i+3]})
			}
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, preview); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
