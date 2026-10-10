//go:build windows && (amd64 || arm64)

package ui

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/app"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

type Action uint32

const (
	Refresh Action = 1001 + iota
	Interval1
	Interval2
	Interval5
	Autostart
	Exit
)

const (
	wmTray   = 0x8001
	wmUpdate = 0x8002
)

var (
	user32           = winapi.DLL("user32.dll")
	kernel32         = winapi.DLL("kernel32.dll")
	shell32          = winapi.DLL("shell32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	unregisterClass  = user32.NewProc("UnregisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	postQuit         = user32.NewProc("PostQuitMessage")
	registerMessage  = user32.NewProc("RegisterWindowMessageW")
	getModule        = kernel32.NewProc("GetModuleHandleW")
	shellNotify      = shell32.NewProc("Shell_NotifyIconW")
	createIcon       = user32.NewProc("CreateIcon")
	destroyIcon      = user32.NewProc("DestroyIcon")
	createMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	destroyMenu      = user32.NewProc("DestroyMenu")
	getCursor        = user32.NewProc("GetCursorPos")
	setForeground    = user32.NewProc("SetForegroundWindow")
	trackMenu        = user32.NewProc("TrackPopupMenu")
	setDPI           = user32.NewProc("SetProcessDpiAwarenessContext")
	messageBox       = user32.NewProc("MessageBoxW")
)

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	_              uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	_              uint32
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}

// NOTIFYICONDATAW，64 位 ABI 大小为 976 字节。
type notifyIcon struct {
	Size                uint32
	_                   uint32
	Window              uintptr
	ID, Flags, Callback uint32
	_                   uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}

type Tray struct {
	hwnd           uintptr
	instance       uintptr
	data           notifyIcon
	icons          map[string]uintptr
	iconCountry    string
	taskbarMessage uint32
	latest         atomic.Pointer[app.Snapshot]
	updatePending  atomic.Bool
	startup        atomic.Bool
	Actions        chan Action
	callback       uintptr
}

func NewTray() *Tray                      { return &Tray{icons: make(map[string]uintptr), Actions: make(chan Action, 8)} }
func (t *Tray) SetAutostart(enabled bool) { t.startup.Store(enabled) }

// Publish 可从任何 goroutine 调用；消息合并避免 UI 消息队列堆积。
func (t *Tray) Publish(s app.Snapshot) {
	t.latest.Store(&s)
	if !t.updatePending.Swap(true) {
		ok, _, _ := postMessage.Call(t.hwnd, wmUpdate, 0, 0)
		if ok == 0 {
			t.updatePending.Store(false)
		}
	}
}

func copyText(dst []uint16, text string) {
	clear(dst)
	u := utf16.Encode([]rune(text))
	if len(u) >= len(dst) {
		u = u[:len(dst)-1]
		if len(u) > 0 && u[len(u)-1] >= 0xd800 && u[len(u)-1] <= 0xdbff {
			u = u[:len(u)-1]
		}
	}
	copy(dst, u)
}

func (t *Tray) callNotify(op uintptr) bool {
	ok, _, _ := shellNotify.Call(op, uintptr(unsafe.Pointer(&t.data)))
	return ok != 0
}

func (t *Tray) addIcon() bool {
	if !t.callNotify(0) {
		return false
	}
	t.data.Version = 4
	t.callNotify(4)
	return true
}

func (t *Tray) update(s app.Snapshot) error {
	country := Country(s)
	if t.iconCountry != country {
		// 新图标就绪后再销毁旧图标，避免 Explorer 持有已释放的 HICON。
		old := t.icons
		t.icons = make(map[string]uintptr)
		t.iconCountry = country
		defer func() {
			for _, h := range old {
				destroyIcon.Call(h)
			}
		}()
	}
	key := IconKey(s)
	icon := t.icons[key]
	if icon == 0 {
		pixels := Pixels(country, s.Status, s.Rates.Up > 0, s.Rates.Down > 0, s.At.Unix()%2 == 0)
		var xor [4096]byte
		var and [128]byte
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				src := (y*32 + x) * 4
				dst := ((31-y)*32 + x) * 4
				xor[dst], xor[dst+1], xor[dst+2], xor[dst+3] = pixels[src+2], pixels[src+1], pixels[src], pixels[src+3]
				if pixels[src+3] == 0 {
					and[(31-y)*4+x/8] |= 1 << uint(7-x%8)
				}
			}
		}
		var err error
		icon, _, err = createIcon.Call(t.instance, 32, 32, 1, 32, uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
		if icon == 0 {
			return winapi.Error("CreateIcon", err)
		}
		t.icons[key] = icon
	}
	t.data.Icon = icon
	copyText(t.data.Tip[:], Tooltip(s))
	if !t.callNotify(1) && !t.addIcon() {
		return fmt.Errorf("无法更新系统托盘图标")
	}
	return nil
}

func (t *Tray) menu() {
	menu, _, _ := createMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	add := func(id Action, label string, flags uintptr) {
		p := winapi.UTF16(label)
		appendMenu.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(p)))
	}
	s := t.latest.Load()
	if s != nil {
		add(0, fmt.Sprintf("GeoPulse [%s] %s", Country(*s), StatusText(s.Status)), 2)
		if s.Geo.IP != "" {
			add(0, "IP: "+s.Geo.IP, 2)
		}
		if s.NIC != "" {
			add(0, "网卡: "+s.NIC, 2)
		}
		if s.GeoRefreshing {
			add(0, "正在查询 IP…", 2)
		}
		if s.GeoError != "" {
			add(0, "定位失败；保留缓存，30 秒内合并刷新", 2)
		}
		if s.WatchError != "" {
			add(0, "网络事件监听失败；仍有 15 分钟保底刷新", 2)
		}
	}
	add(0, "", 0x800)
	add(Refresh, "立即刷新 IP（最短间隔 30 秒）", 0)
	interval := 1
	if s != nil {
		interval = s.IntervalSeconds
	}
	for i, n := range []int{1, 2, 5} {
		flags := uintptr(0)
		if interval == n {
			flags = 8
		}
		add(Action(uint32(Interval1)+uint32(i)), fmt.Sprintf("检测/显示间隔 %ds（心跳固定 1s）", n), flags)
	}
	flags := uintptr(0)
	if t.startup.Load() {
		flags = 8
	}
	add(Autostart, "开机自启", flags)
	add(0, "", 0x800)
	add(Exit, "退出", 0)
	var p point
	getCursor.Call(uintptr(unsafe.Pointer(&p)))
	setForeground.Call(t.hwnd)
	command, _, _ := trackMenu.Call(menu, 0x100|2, uintptr(p.X), uintptr(p.Y), 0, t.hwnd, 0)
	postMessage.Call(t.hwnd, 0, 0, 0)
	t.callNotify(3)
	if command != 0 {
		select {
		case t.Actions <- Action(command):
		default:
		}
	}
}

func (t *Tray) windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if msg == t.taskbarMessage && t.taskbarMessage != 0 {
		t.addIcon()
		return 0
	}
	switch msg {
	case wmUpdate:
		t.updatePending.Store(false)
		if s := t.latest.Load(); s != nil {
			_ = t.update(*s)
		}
		return 0
	case wmTray:
		event := uint32(lparam & 0xffff)
		if event == 0x7b || event == 0x205 || event == 0x202 || event == 0x400 || event == 0x401 {
			t.menu()
		}
		return 0
	case 0x10:
		destroyWindow.Call(hwnd)
		return 0
	case 0x2:
		postQuit.Call(0)
		return 0
	}
	r, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

// Run 必须运行在锁定的主 OS 线程上；隐藏顶层窗口只承载 Win32 消息。
// 不 ShowWindow，不创建任务栏按钮，也不使用消息专用窗口，以接收 Explorer 重启广播。
func (t *Tray) Run(ctx context.Context, ready func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if setDPI.Find() == nil {
		setDPI.Call(^uintptr(3))
	}
	t.instance, _, _ = getModule.Call(0)
	t.callback = syscall.NewCallback(t.windowProc)
	name := winapi.UTF16("GeoPulseHiddenWindow")
	wc := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), Procedure: t.callback, Instance: t.instance, ClassName: name}
	if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return winapi.Error("RegisterClassExW", err)
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(name)), t.instance)
	hwnd, _, err := createWindow.Call(0x80, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, t.instance, 0)
	if hwnd == 0 {
		return winapi.Error("CreateWindowExW", err)
	}
	t.hwnd = hwnd
	defer destroyWindow.Call(hwnd)
	taskbar := winapi.UTF16("TaskbarCreated")
	r, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(taskbar)))
	t.taskbarMessage = uint32(r)
	t.data = notifyIcon{Size: uint32(unsafe.Sizeof(notifyIcon{})), Window: hwnd, ID: 1, Flags: 1 | 2 | 4 | 0x80, Callback: wmTray}
	defer func() {
		t.callNotify(2)
		for _, icon := range t.icons {
			destroyIcon.Call(icon)
		}
	}()
	if err := t.update(app.Snapshot{Status: monitor.Unknown}); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { postMessage.Call(hwnd, 0x10, 0, 0) })
	defer stop()
	ready()
	var msg message
	for {
		r, _, err := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 {
			return winapi.Error("GetMessageW", err)
		}
		if r == 0 {
			return nil
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func ShowError(err error) {
	text, title := winapi.UTF16(err.Error()), winapi.UTF16("GeoPulse")
	messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
