// Package ui 将状态转为托盘文字与图标；绘制不依赖字体或外部图片。
package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hrthas-king/GeoPulse/internal/app"
	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
)

func Country(s app.Snapshot) string {
	if s.Status == monitor.Offline || !ipinfo.ValidCountry(s.Geo.Country) {
		return "--"
	}
	return s.Geo.Country
}

func StatusText(status monitor.Status) string {
	switch status {
	case monitor.Online:
		return "正常"
	case monitor.Degraded:
		return "待确认"
	case monitor.Offline:
		return "断网警告"
	default:
		return "检测中"
	}
}

func Speed(bytes float64) string {
	if bytes < 0 || math.IsNaN(bytes) || math.IsInf(bytes, 0) {
		return "--"
	}
	units := []string{"B/s", "KiB/s", "MiB/s", "GiB/s"}
	i := 0
	for bytes >= 1024 && i < len(units)-1 {
		bytes /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", bytes, units[i])
	}
	return fmt.Sprintf("%.1f %s", bytes, units[i])
}

func RTT(s app.Snapshot) string {
	if !s.Probe.Success {
		return "--"
	}
	if s.Probe.RTT < time.Millisecond {
		return "<1 ms"
	}
	return fmt.Sprintf("%d ms", s.Probe.RTT.Milliseconds())
}

func Tooltip(s app.Snapshot) string {
	ip := s.Geo.IP
	if ip == "" {
		ip = "查询中/未知"
	}
	if s.GeoError != "" || s.Status == monitor.Offline {
		ip += "*"
	}
	up, down := "--", "--"
	if s.Rates.Valid {
		up, down = Speed(s.Rates.Up), Speed(s.Rates.Down)
	}
	evidence := "上行待确认"
	if s.Probe.Uplink == monitor.UplinkConfirmed {
		evidence = "上下行已验证"
	}
	if s.Probe.Uplink == monitor.UplinkLocalError {
		evidence = "本地发送错误"
	}
	return fmt.Sprintf("GeoPulse [%s] %s\nIP: %s\n↑ %s  ↓ %s\nRTT: %s\n%s", Country(s), StatusText(s.Status), ip, up, down, RTT(s), evidence)
}

// 5×7 点阵字形，保证托盘缩小后两位国家代码仍有一致形状。
var glyphs = map[byte][7]byte{
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14},
	'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16},
	'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'I': {31, 4, 4, 4, 4, 4, 31},
	'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31},
	'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 21, 19, 17, 17, 17}, 'O': {14, 17, 17, 17, 17, 17, 14},
	'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17},
	'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14},
	'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17},
	'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31}, '-': {0, 0, 0, 31, 0, 0, 0},
}

// 返回顶向下的 RGBA 像素；Win32 创建图标时转换为底向上的 BGRA。
func Pixels(country string, status monitor.Status, up, down, pulse bool) []byte {
	if len(country) != 2 || (country != "--" && !ipinfo.ValidCountry(country)) {
		country = "--"
	}
	pixels := make([]byte, 32*32*4)
	set := func(x, y int, r, g, b byte) {
		i := (y*32 + x) * 4
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = r, g, b, 255
	}
	for y := 1; y < 31; y++ {
		for x := 1; x < 31; x++ {
			set(x, y, 23, 32, 49)
		}
	}
	for n := 0; n < 2; n++ {
		glyph := glyphs[country[n]]
		for y, row := range glyph {
			for x := 0; x < 5; x++ {
				if row&(1<<uint(4-x)) != 0 {
					for dy := 0; dy < 2; dy++ {
						for dx := 0; dx < 2; dx++ {
							set(5+n*12+x*2+dx, 4+y*2+dy, 240, 246, 255)
						}
					}
				}
			}
		}
	}
	r, g, b := byte(148), byte(163), byte(184)
	switch status {
	case monitor.Online:
		r, g, b = 34, 197, 94
	case monitor.Offline:
		r, g, b = 239, 68, 68
	case monitor.Degraded:
		r, g, b = 245, 158, 11
	}
	for y := 21; y <= 29; y++ {
		for x := 12; x <= 20; x++ {
			if (x-16)*(x-16)+(y-25)*(y-25) <= 16 {
				set(x, y, r, g, b)
			}
		}
	}
	// 左上行、右下行箭头在有流量时随秒钟微闪，静止时保留暗轮廓。
	for n, active := range []bool{up, down} {
		color := byte(92)
		if active {
			color = 170
			if pulse {
				color = 255
			}
		}
		x := 5 + n*22
		for y := 22; y <= 28; y++ {
			set(x, y, 56, color, color)
		}
		for d := 1; d <= 2; d++ {
			y := 22 + d
			if n == 1 {
				y = 28 - d
			}
			set(x-d, y, 56, color, color)
			set(x+d, y, 56, color, color)
		}
	}
	return pixels
}

func IconKey(s app.Snapshot) string {
	return strings.Join([]string{Country(s), string(s.Status), fmt.Sprint(s.Rates.Up > 0, s.Rates.Down > 0, s.At.Unix()%2 == 0)}, "/")
}
