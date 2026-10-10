//go:build windows && (amd64 || arm64)

// Package network 使用 IP Helper API，不启动 ping.exe，不创建 raw socket。
package network

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/monitor"
	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

var (
	iphlp            = winapi.DLL("iphlpapi.dll")
	getBestInterface = iphlp.NewProc("GetBestInterface")
	getIfEntry2      = iphlp.NewProc("GetIfEntry2")
	icmpCreate       = iphlp.NewProc("IcmpCreateFile")
	icmpSend         = iphlp.NewProc("IcmpSendEcho")
	icmpClose        = iphlp.NewProc("IcmpCloseHandle")
	notifyInterface  = iphlp.NewProc("NotifyIpInterfaceChange")
	notifyAddress    = iphlp.NewProc("NotifyUnicastIpAddressChange")
	notifyRoute      = iphlp.NewProc("NotifyRouteChange2")
	cancelNotify     = iphlp.NewProc("CancelMibChangeNotify2")
)

// 对照 netioapi.h 的 MIB_IF_ROW2；amd64/arm64 均采用 8 字节自然对齐。
// 结构布局有测试保护，误用 32 位编译会被 build tag 阻止。
type ifRow2 struct {
	LUID                                                                            uint64
	Index                                                                           uint32
	GUID                                                                            [16]byte
	Alias                                                                           [257]uint16
	Description                                                                     [257]uint16
	PhysicalAddressLength                                                           uint32
	PhysicalAddress                                                                 [32]byte
	PermanentPhysicalAddress                                                        [32]byte
	MTU, Type, TunnelType, MediaType, PhysicalMediumType, AccessType, DirectionType uint32
	Flags                                                                           uint8
	_                                                                               [3]byte
	OperStatus, AdminStatus, MediaConnectState                                      uint32
	NetworkGUID                                                                     [16]byte
	ConnectionType                                                                  uint32
	_                                                                               [4]byte
	TransmitLinkSpeed, ReceiveLinkSpeed                                             uint64
	InOctets, InUcastPkts, InNUcastPkts, InDiscards, InErrors, InUnknownProtos      uint64
	InUcastOctets, InMulticastOctets, InBroadcastOctets                             uint64
	OutOctets, OutUcastPkts, OutNUcastPkts, OutDiscards, OutErrors                  uint64
	OutUcastOctets, OutMulticastOctets, OutBroadcastOctets, OutQLen                 uint64
}

func ipv4(target string) (uint32, error) {
	addr, err := netip.ParseAddr(target)
	if err != nil || !addr.Is4() {
		return 0, fmt.Errorf("无效 IPv4 探针目标 %q", target)
	}
	a := addr.As4()
	return binary.LittleEndian.Uint32(a[:]), nil
}

// 每秒只读有效路由的接口计数，防止 VPN 虚拟接口与物理接口双重计数。
// 它是出口网卡总流量（含局域网/其他进程），不是应用流量或主动测速。
func ReadCounters(target string) (monitor.Counters, error) {
	ip, err := ipv4(target)
	if err != nil {
		return monitor.Counters{}, err
	}
	var index uint32
	code, _, _ := getBestInterface.Call(uintptr(ip), uintptr(unsafe.Pointer(&index)))
	if code != 0 {
		return monitor.Counters{}, fmt.Errorf("找不到 IPv4 出口路由: %w", syscall.Errno(code))
	}
	row := ifRow2{Index: index}
	code, _, _ = getIfEntry2.Call(uintptr(unsafe.Pointer(&row)))
	if code != 0 {
		return monitor.Counters{}, fmt.Errorf("读取网卡计数: %w", syscall.Errno(code))
	}
	if row.OperStatus != 1 {
		return monitor.Counters{}, fmt.Errorf("出口网卡未连接")
	}
	return monitor.Counters{LUID: row.LUID, Index: row.Index, Name: syscall.UTF16ToString(row.Alias[:]),
		Sent: row.OutOctets, Received: row.InOctets, At: time.Now()}, nil
}

type Pinger struct {
	handle  uintptr
	payload [32]byte
	// uint64 数组确保返回的 ICMP 结构有正确对齐，不需每次分配。
	reply [256]uint64
}

func NewPinger() (*Pinger, error) {
	h, _, err := icmpCreate.Call()
	if h == ^uintptr(0) || h == 0 {
		return nil, winapi.Error("IcmpCreateFile", err)
	}
	p := &Pinger{handle: h}
	copy(p.payload[:], "GeoPulse connectivity heartbeat")
	return p, nil
}

func (p *Pinger) Close() {
	if p.handle != 0 {
		icmpClose.Call(p.handle)
		p.handle = 0
	}
}

// 同步系统调用只在独立工作 goroutine 中运行，650ms 超时小于 1s 心跳周期。
// 单次只发一个 32 字节 payload，失败在下一秒换备用地址，不加倍发包。
func (p *Pinger) Ping(target string) monitor.Probe {
	result := monitor.Probe{Target: target, Uplink: monitor.UplinkUnknown}
	defer func() { runtime.KeepAlive(p) }()
	ip, err := ipv4(target)
	if err != nil {
		result.Uplink = monitor.UplinkLocalError
		result.Detail = err.Error()
		result.At = time.Now()
		return result
	}
	if _, err = ReadCounters(target); err != nil {
		result.Uplink = monitor.UplinkLocalError
		result.Detail = err.Error()
		result.At = time.Now()
		return result
	}
	n, _, callErr := icmpSend.Call(p.handle, uintptr(ip), uintptr(unsafe.Pointer(&p.payload[0])), 32, 0,
		uintptr(unsafe.Pointer(&p.reply[0])), uintptr(unsafe.Sizeof(p.reply)), 650)
	result.At = time.Now()
	if n == 0 {
		// 超时不能证明本地发送失败；路由器也可能返回不可达，仍保留未知。
		if callErr == syscall.Errno(5) || callErr == syscall.Errno(10013) || callErr == syscall.Errno(11050) {
			result.Uplink = monitor.UplinkLocalError
		}
		result.Detail = fmt.Sprintf("ICMP 未响应/失败: %v", callErr)
		return result
	}
	words := (*[4]uint32)(unsafe.Pointer(&p.reply[0]))
	status := words[1]
	dataSize := uint16(words[3])
	if status != 0 || dataSize < 32 {
		result.Detail = fmt.Sprintf("ICMP 状态 %d，响应数据 %d 字节", status, dataSize)
		return result
	}
	result.Success = true
	result.Uplink = monitor.UplinkConfirmed
	result.RTT = time.Duration(words[2]) * time.Millisecond
	return result
}

type Watcher struct {
	Events   chan struct{}
	handles  []uintptr
	callback uintptr
}

// 回调可能发生在 Windows 线程上，只投递无阻塞信号，不访问临时 Row 指针。
// 接口、单播地址、路由都监听：覆盖断网重连、DHCP 和 VPN 默认路由变化。
func Watch() (*Watcher, error) {
	w := &Watcher{Events: make(chan struct{}, 1)}
	w.callback = syscall.NewCallback(func(context, row, kind uintptr) uintptr {
		select {
		case w.Events <- struct{}{}:
		default:
		}
		return 0
	})
	for _, proc := range []*syscall.LazyProc{notifyInterface, notifyAddress, notifyRoute} {
		var handle uintptr
		code, _, _ := proc.Call(0, w.callback, 0, 0, uintptr(unsafe.Pointer(&handle)))
		if code != 0 {
			w.Close()
			return w, fmt.Errorf("%s: %w", proc.Name, syscall.Errno(code))
		}
		w.handles = append(w.handles, handle)
	}
	return w, nil
}

// CancelMibChangeNotify2 会等待回调完成；不可在回调内部调用。
func (w *Watcher) Close() {
	for _, h := range w.handles {
		cancelNotify.Call(h)
	}
	w.handles = nil
	runtime.KeepAlive(w)
}
