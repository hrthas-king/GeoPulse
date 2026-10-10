//go:build windows && (amd64 || arm64)

package network

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/monitor"
	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

var (
	getIfTable2    = iphlp.NewProc("GetIfTable2")
	freeMibTable   = iphlp.NewProc("FreeMibTable")
	getTCPTable    = iphlp.NewProc("GetExtendedTcpTable")
	getUDPTable    = iphlp.NewProc("GetExtendedUdpTable")
	egressKernel   = winapi.DLL("kernel32.dll")
	createSnapshot = egressKernel.NewProc("CreateToolhelp32Snapshot")
	processFirst   = egressKernel.NewProc("Process32FirstW")
	processNext    = egressKernel.NewProc("Process32NextW")
	closeSnapshot  = egressKernel.NewProc("CloseHandle")
	globalFree     = egressKernel.NewProc("GlobalFree")
	getUserProxy   = winapi.DLL("winhttp.dll").NewProc("WinHttpGetIEProxyConfigForCurrentUser")
)

type processEntry struct {
	Size, Usage, PID        uint32
	_                       uint32
	Heap                    uintptr
	Module, Threads, Parent uint32
	Priority                int32
	Flags                   uint32
	Name                    [260]uint16
}

func proxyCore(name string) bool {
	switch strings.ToLower(name) {
	case "nano.exe", "phantom.exe", "mihomo.exe", "verge-mihomo.exe", "clash.exe", "clash-meta.exe",
		"sing-box.exe", "xray.exe", "v2ray.exe", "hiddify-core.exe", "nekobox_core.exe":
		return true
	}
	return false
}

func proxyPIDs() map[uint32]bool {
	pids := make(map[uint32]bool)
	h, _, _ := createSnapshot.Call(2, 0)
	if h == ^uintptr(0) || h == 0 {
		return pids
	}
	defer closeSnapshot.Call(h)
	entry := processEntry{Size: uint32(unsafe.Sizeof(processEntry{}))}
	ok, _, _ := processFirst.Call(h, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if proxyCore(syscall.UTF16ToString(entry.Name[:])) {
			pids[entry.PID] = true
		}
		ok, _, _ = processNext.Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	return pids
}

func pointerText(p *uint16) string {
	if p == nil {
		return ""
	}
	// Windows 返回受信任的 NUL 结尾字符串；只读到结束，不复制周围内存。
	data := (*[32768]uint16)(unsafe.Pointer(p))
	i := 0
	for i < len(data) && data[i] != 0 {
		i++
	}
	return syscall.UTF16ToString(data[:i])
}

func localEgressMode() (bool, string) {
	active := false
	parts := make([]string, 0, 4)
	var proxy struct {
		AutoDetect              uint32
		_                       uint32
		AutoURL, Server, Bypass *uint16
	}
	if ok, _, _ := getUserProxy.Call(uintptr(unsafe.Pointer(&proxy))); ok != 0 {
		server, pac := pointerText(proxy.Server), pointerText(proxy.AutoURL)
		active = server != "" || pac != ""
		parts = append(parts, server, pac, pointerText(proxy.Bypass))
		for _, p := range []*uint16{proxy.AutoURL, proxy.Server, proxy.Bypass} {
			if p != nil {
				globalFree.Call(uintptr(unsafe.Pointer(p)))
			}
		}
	}
	var table unsafe.Pointer
	if code, _, _ := getIfTable2.Call(uintptr(unsafe.Pointer(&table))); code == 0 && table != nil {
		defer freeMibTable.Call(uintptr(table))
		count := *(*uint32)(table)
		if count <= 1024 {
			rows := unsafe.Slice((*ifRow2)(unsafe.Add(table, 8)), int(count))
			for i := range rows {
				row := &rows[i]
				description := strings.ToLower(syscall.UTF16ToString(row.Description[:]))
				if row.OperStatus == 1 && (row.Type == 131 || row.Type == 53 || strings.Contains(description, "tunnel") || strings.Contains(description, "wintun") || strings.Contains(description, "wireguard")) {
					active = true
					var luid [8]byte
					binary.LittleEndian.PutUint64(luid[:], row.LUID)
					parts = append(parts, string(luid[:]))
				}
			}
		}
	}
	sort.Strings(parts)
	return active, strings.Join(parts, "\x00")
}

type socketReader struct{ buffer []byte }

func (r *socketReader) read(proc *syscall.LazyProc, family, class uintptr) []byte {
	if len(r.buffer) == 0 {
		r.buffer = make([]byte, 4096)
	}
	for attempt := 0; attempt < 2; attempt++ {
		size := uint32(len(r.buffer))
		code, _, _ := proc.Call(uintptr(unsafe.Pointer(&r.buffer[0])), uintptr(unsafe.Pointer(&size)), 0, family, class, 0)
		if code == 0 {
			return r.buffer[:size]
		}
		if code != 122 || size > 1<<20 {
			return nil
		}
		r.buffer = make([]byte, size)
	}
	return nil
}

// 只记录代理内核的端点集合，不记录浏览器连接、请求内容或凭据。
// TCP 忽略本地临时端口，普通建立更多到同一节点的连接不会制造变化。
func socketKeys(data []byte, ipv6, udp bool, pids map[uint32]bool) []string {
	if len(data) < 4 {
		return nil
	}
	rowSize := 24
	if ipv6 {
		rowSize = 56
	}
	if udp {
		rowSize = 12
		if ipv6 {
			rowSize = 28
		}
	}
	count := int(binary.LittleEndian.Uint32(data[:4]))
	if count > (len(data)-4)/rowSize {
		return nil
	}
	keys := make([]string, 0, count)
	for i := 0; i < count; i++ {
		row := data[4+i*rowSize : 4+(i+1)*rowSize]
		pid := binary.LittleEndian.Uint32(row[rowSize-4:])
		if !pids[pid] {
			continue
		}
		if udp {
			// UDP 无远端字段；仅用代理内核的本地 socket 重建作为变化提示。
			keys = append(keys, "u"+string(row))
		} else if ipv6 {
			if binary.LittleEndian.Uint32(row[48:52]) == 5 {
				keys = append(keys, "6"+string(row[24:48])+string(row[52:56]))
			}
		} else if binary.LittleEndian.Uint32(row[:4]) == 5 {
			keys = append(keys, "4"+string(row[12:24]))
		}
	}
	return keys
}

func egressHash(mode string, keys []string) uint64 {
	sort.Strings(keys)
	h := fnv.New64a()
	h.Write([]byte(mode))
	last := ""
	for _, key := range keys {
		if key != last {
			h.Write([]byte{0})
			h.Write([]byte(key))
			last = key
		}
	}
	return h.Sum64()
}

// WatchEgress 每秒只读本地配置、TUN 网卡与代理 socket 元数据，零公网探针。
// 专有客户端在同一 socket 内更换出口时无法观察，app 的 30 秒复核兜底。
func WatchEgress(ctx context.Context) <-chan monitor.EgressEvent {
	events := make(chan monitor.EgressEvent, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var reader socketReader
		var previous uint64
		var previousActive, initialized bool
		var candidate uint64
		var stable int
		for {
			if ctx.Err() != nil {
				return
			}
			active, mode := localEgressMode()
			var keys []string
			if active {
				pids := proxyPIDs()
				if len(pids) > 0 {
					for _, family := range []uintptr{2, 23} {
						keys = append(keys, socketKeys(reader.read(getTCPTable, family, 5), family == 23, false, pids)...)
						keys = append(keys, socketKeys(reader.read(getUDPTable, family, 1), family == 23, true, pids)...)
					}
				}
			}
			current := egressHash(mode, keys)
			if current == candidate {
				stable++
			} else {
				candidate = current
				stable = 1
			}
			// 等本地端点稳定两个样本，过滤短连接与节点重连的瞬时波动。
			if !initialized || (stable >= 2 && (current != previous || active != previousActive)) {
				event := monitor.EgressEvent{Active: active, Changed: initialized}
				select {
				case events <- event:
				default:
					select {
					case old := <-events:
						event.Changed = event.Changed || old.Changed
					default:
					}
					select {
					case events <- event:
					default:
					}
				}
				previous, previousActive, initialized = current, active, true
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return events
}
