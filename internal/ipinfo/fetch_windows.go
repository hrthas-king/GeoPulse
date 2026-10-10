//go:build windows && (amd64 || arm64)

package ipinfo

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

var (
	winhttp         = winapi.DLL("winhttp.dll")
	open            = winhttp.NewProc("WinHttpOpen")
	connect         = winhttp.NewProc("WinHttpConnect")
	openRequest     = winhttp.NewProc("WinHttpOpenRequest")
	setTimeouts     = winhttp.NewProc("WinHttpSetTimeouts")
	setOption       = winhttp.NewProc("WinHttpSetOption")
	sendRequest     = winhttp.NewProc("WinHttpSendRequest")
	receiveResponse = winhttp.NewProc("WinHttpReceiveResponse")
	queryHeaders    = winhttp.NewProc("WinHttpQueryHeaders")
	readData        = winhttp.NewProc("WinHttpReadData")
	closeHandle     = winhttp.NewProc("WinHttpCloseHandle")
)

// Fetch 由事件/有限频率工作队列调用。使用域名，跟随当前系统路由和代理规则。
// WinHTTP 使用系统 TLS 验证，同时继承当前用户的系统代理配置。
// 同步请求在后台执行；绝不跨线程关闭同步句柄（WinHTTP 禁止这种竞态）。
// 取消在每个有界系统调用之间检查，迟到结果由 app 的网络代次校验丢弃。
func Fetch(ctx context.Context) (Info, error) {
	var lastErr error
	for _, host := range []string{"www.cloudflare.com", "cloudflare.com"} {
		if err := ctx.Err(); err != nil {
			return Info{}, err
		}
		body, err := fetchHost(ctx, host)
		if err == nil {
			info, parseErr := ParseTrace(body)
			if parseErr == nil {
				return info, nil
			}
			err = parseErr
		}
		lastErr = fmt.Errorf("%s: %w", host, err)
	}
	return Info{}, lastErr
}

func fetchHost(ctx context.Context, host string) ([]byte, error) {
	agent := winapi.UTF16("GeoPulse/1.0")
	// AUTOMATIC_PROXY=4；NO_PROXY=1 不能代表浏览器使用的系统代理出口。
	session, _, err := open.Call(uintptr(unsafe.Pointer(agent)), 4, 0, 0, 0)
	if session == 0 {
		return nil, winapi.Error("WinHttpOpen", err)
	}
	defer closeHandle.Call(session)
	// DNS/connect/send/receive 每阶段都有上限，避免离线时长期挂起。
	if ok, _, err := setTimeouts.Call(session, 800, 1200, 1200, 1200); ok == 0 {
		return nil, winapi.Error("WinHttpSetTimeouts", err)
	}
	hostPtr := winapi.UTF16(host)
	conn, _, err := connect.Call(session, uintptr(unsafe.Pointer(hostPtr)), 443, 0)
	if conn == 0 {
		return nil, winapi.Error("WinHttpConnect", err)
	}
	defer closeHandle.Call(conn)
	verb, path := winapi.UTF16("GET"), winapi.UTF16("/cdn-cgi/trace")
	request, _, err := openRequest.Call(conn, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(path)), 0, 0, 0, 0x00800000)
	if request == 0 {
		return nil, winapi.Error("WinHttpOpenRequest", err)
	}
	defer closeHandle.Call(request)
	// 禁用重定向、cookie 与 keep-alive。每次重新连接，避免节点切换后
	// 沿用指向旧出口的 TCP/TLS 连接；仍然保留完整证书验证。
	var disable uint32 = 2 | 1 | 8
	if ok, _, err := setOption.Call(request, 63, uintptr(unsafe.Pointer(&disable)), 4); ok == 0 {
		return nil, winapi.Error("WinHttpSetOption", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ok, _, err := sendRequest.Call(request, 0, 0, 0, 0, 0, 0); ok == 0 {
		return nil, winapi.Error("WinHttpSendRequest", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ok, _, err := receiveResponse.Call(request, 0); ok == 0 {
		return nil, winapi.Error("WinHttpReceiveResponse", err)
	}
	var status uint32
	size := uint32(4)
	if ok, _, err := queryHeaders.Call(request, 19|0x20000000, 0, uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&size)), 0); ok == 0 {
		return nil, winapi.Error("WinHttpQueryHeaders", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	body := make([]byte, 0, 512)
	var chunk [512]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var n uint32
		ok, _, err := readData.Call(request, uintptr(unsafe.Pointer(&chunk[0])), uintptr(len(chunk)), uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			return nil, winapi.Error("WinHttpReadData", err)
		}
		if n == 0 {
			break
		}
		if n > uint32(len(chunk)) || len(body)+int(n) > MaxBody {
			return nil, fmt.Errorf("定位响应超出限制")
		}
		body = append(body, chunk[:n]...)
	}
	runtime.KeepAlive(agent)
	runtime.KeepAlive(hostPtr)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(path)
	return body, nil
}
