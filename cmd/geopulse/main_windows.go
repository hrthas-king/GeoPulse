//go:build windows && (amd64 || arm64)

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/hrthas-king/GeoPulse/internal/app"
	"github.com/hrthas-king/GeoPulse/internal/config"
	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/network"
	"github.com/hrthas-king/GeoPulse/internal/startup"
	"github.com/hrthas-king/GeoPulse/internal/ui"
	"github.com/hrthas-king/GeoPulse/internal/winapi"
)

var version = "dev"

func main() {
	diagnose := flag.Duration("diagnose", 0, "后台诊断持续时间，例如 15s（不显示托盘）")
	runFor := flag.Duration("run-for", 0, "托盘冒烟测试后自动退出，例如 15s")
	output := flag.String("output", "", "诊断 JSON 输出路径（包含公网 IP，请勿公开提交）")
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Parse()
	if *showVersion {
		fmt.Println("GeoPulse", version)
		return
	}
	if *diagnose < 0 || *runFor < 0 {
		fmt.Fprintln(os.Stderr, "持续时间不能为负数")
		os.Exit(1)
	}
	// 控制 Go 自身堆目标；该值不限制 Win32、TLS 或整个进程的工作集。
	// Go 计算量很小；阻塞的系统调用仍会释放 P，不会卡住其他协程。
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(8 << 20)
	debug.SetGCPercent(50)
	err := run(*diagnose, *runFor, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if *diagnose == 0 && *runFor == 0 {
			ui.ShowError(err)
		}
		os.Exit(1)
	}
}

func run(diagnose, runFor time.Duration, output string) error {
	if diagnose == 0 {
		release, exists, err := winapi.SingleInstance()
		if err != nil {
			return err
		}
		defer release()
		if exists {
			return nil
		}
	}
	pinger, err := network.NewPinger()
	if err != nil {
		return err
	}
	defer pinger.Close()
	watcher, watchErr := network.Watch()
	defer watcher.Close()
	watchError := ""
	if watchErr != nil {
		watchError = watchErr.Error()
	}
	engine := app.New(app.Dependencies{ReadCounters: network.ReadCounters, Ping: pinger.Ping, Fetch: ipinfo.Fetch,
		NetworkEvents: watcher.Events, WatchError: watchError})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var latest atomic.Pointer[app.Snapshot]
	settings := config.Load()
	started := time.Now()
	var trayMemory winapi.Memory
	if diagnose > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, diagnose)
		defer stop()
		engine.Run(ctx, settings.IntervalSeconds, func(s app.Snapshot) { latest.Store(&s) })
	} else {
		if runFor > 0 {
			var stop context.CancelFunc
			ctx, stop = context.WithTimeout(ctx, runFor)
			defer stop()
		}
		tray := ui.NewTray()
		if enabled, err := startup.Enabled(); err == nil {
			tray.SetAutostart(enabled)
		}
		done := make(chan struct{})
		startedEngine := false
		err = tray.Run(ctx, func() {
			startedEngine = true
			go func() {
				engine.Run(ctx, settings.IntervalSeconds, func(s app.Snapshot) {
					latest.Store(&s)
					tray.Publish(s)
					// 冒烟测试在托盘还存在时记录资源计数。
					if runFor > 0 {
						if mem, err := winapi.ProcessMemory(); err == nil {
							trayMemory = mem
						}
					}
				})
				close(done)
			}()
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case action := <-tray.Actions:
						switch action {
						case ui.Exit:
							cancel()
						case ui.Refresh:
							select {
							case engine.Refresh <- struct{}{}:
							default:
							}
						case ui.Interval1, ui.Interval2, ui.Interval5:
							n := map[ui.Action]int{ui.Interval1: 1, ui.Interval2: 2, ui.Interval5: 5}[action]
							select {
							case engine.Interval <- n:
							default:
							}
							if err := config.Save(config.Config{IntervalSeconds: n}); err != nil {
								ui.ShowError(err)
							}
						case ui.Autostart:
							enabled, err := startup.Enabled()
							if err == nil {
								err = startup.Set(!enabled)
							}
							if err != nil {
								ui.ShowError(err)
							} else {
								tray.SetAutostart(!enabled)
							}
						}
					}
				}
			}()
		})
		cancel()
		if startedEngine {
			<-done
		}
		if err != nil {
			return err
		}
	}
	if output != "" || diagnose > 0 {
		memory, err := winapi.ProcessMemory()
		if err != nil {
			return err
		}
		report := struct {
			Version        string        `json:"version"`
			GoVersion      string        `json:"go_version"`
			Arch           string        `json:"arch"`
			ElapsedSeconds float64       `json:"elapsed_seconds"`
			Tray           bool          `json:"tray_started"`
			Snapshot       *app.Snapshot `json:"snapshot"`
			Memory         winapi.Memory `json:"memory"`
			TrayMemory     winapi.Memory `json:"memory_with_tray"`
		}{version, runtime.Version(), runtime.GOARCH, time.Since(started).Seconds(), diagnose == 0, latest.Load(), memory, trayMemory}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if output != "" {
			return os.WriteFile(output, data, 0600)
		}
		fmt.Println(string(data))
	}
	return nil
}
