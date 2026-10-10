# GeoPulse

![Windows](https://img.shields.io/badge/Windows-10%20%2F%2011-0078D4)
![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8)
![CGO](https://img.shields.io/badge/CGO-disabled-brightgreen)
![License](https://img.shields.io/badge/license-MIT-blue)
[![CI](https://github.com/hrthas-king/GeoPulse/actions/workflows/ci.yml/badge.svg)](https://github.com/hrthas-king/GeoPulse/actions/workflows/ci.yml)

**秒级网络感知，极低带宽开销的 Windows 托盘 IP / 国家监控工具。**

双击单个 `.exe` 即静默驻留，无控制台、无任务栏窗口、无需管理员或运行时安装。Go 直接调用 Win32 托盘、IP Helper 和 WinHTTP API；不使用 CGo、Electron、WinForms 或外部 `ping.exe`，第三方 Go 依赖为零。

## 功能

- 图标内显示两位国家代码，以及绿色正常、黄色待确认、红色断网、灰色初始化状态；离线显示 `--`。
- 左侧上行、右侧下行箭头随轻微流量闪动，图标缓存有上限并释放 Win32 资源。
- 悬停显示公网 IP、国家、上行 / 下行实际速率、ICMP RTT，以及上行证据状态。
- 网卡 64 位 IO 计数固定 **每秒采样**，按真实经过时间计算 B/s；切换接口、计数回滚或网卡不可用时重建基线。
- **每秒最多一个** 32 字节 ICMP payload，650ms 超时；失败下一秒切换 `1.1.1.1` / `8.8.8.8`，连续两次失败触发断网警告，收到成功响应立即恢复。
- 监听系统接口、单播地址、路由变化；750ms 合并事件、自动查询最短 30s。TUN/代理模式另外观察本地端点变化，并每 30s 复核出口；普通模式仍为 15 分钟保底。
- 右键：刷新 IP、1s / 2s / 5s 检测显示间隔、当前用户开机自启、退出。设置保存在 `%LOCALAPPDATA%\GeoPulse\config.json`。
- Explorer 重启后自动重建托盘图标；命名互斥体避免同一登录会话重复驻留。

**带宽说明：**秒级探针不可能消耗零带宽。一次 IPv4 Echo 请求 / 回复各约 60 字节（32 payload + 8 ICMP + 20 IPv4），合计约 **120 B/s、0.96 kbit/s**，不含链路层、VPN 封装及低频 HTTPS 握手。GeoPulse 不进行主动测速、大文件下载或每秒访问 IP API。

**内存说明：**10 MB 是资源目标，不能以 Go 堆软限制替代整个进程的硬性保证。Windows 共享 DLL、TLS、驱动与系统版本都会影响占用。实测数据、测量口径与未达成的限制见 [验证记录](docs/VALIDATION.md)。

**v0.1.1 修复：**图标朝向恢复正常，TUN 内切换节点可以更新地区。手动刷新最短合并间隔为 2 秒；没有可观察本地切换信号的客户端，自动更新最多等待约 30 秒加查询时间。[更新记录](docs/CHANGELOG.md)

## 功能截图预留位

> 这里预留真实 Windows 托盘、Hover Tooltip 和右键菜单截图。
> 将脱敏截图保存为 `docs/screenshots/tray.png`、`tooltip.png`、`menu.png` 后添加 Markdown 图片链接。
> 公网 IP 建议替换为文档示例地址再公开提交。

## 下载与运行

从 [Releases](https://github.com/hrthas-king/GeoPulse/releases) 获取 ZIP，解压后运行其中的 `.exe`：

- Intel / AMD 64 位 Windows：`windows-amd64`。
- Windows on ARM：`windows-arm64`。

应用图标可能位于 Windows 的托盘折叠区域，可拖到常驻区域。退出通过右键菜单完成。开机自启默认关闭，仅在用户点菜单时写入当前用户的 `Run\GeoPulse` 注册表值。

本项目仅支持 **Windows 10 / 11 的 amd64、arm64**。发布文件未做商业代码签名。

## 快速构建

安装 [Go 官方工具链](https://go.dev/dl/)（最低 1.24；CI 使用 1.27.2）和 Git，然后在 PowerShell 执行：

```powershell
git clone https://github.com/hrthas-king/GeoPulse.git
cd GeoPulse
go test ./...
go vet ./...
./scripts/build.ps1 -Arch amd64 -Version dev
./dist/GeoPulse-windows-amd64.exe
```

直接单文件编译，不需要 C 编译器或 `.dll` 附件：

```powershell
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=v0.1.0" -o dist/GeoPulse.exe ./cmd/geopulse
```

`-s -w` 去掉符号与调试信息，`-H=windowsgui` 去掉控制台，`-trimpath` 去掉构建机器路径。更换 `$env:GOARCH = 'arm64'` 即可交叉编译 ARM64。依赖均来自 Go 标准库，故没有 `go.sum`。

验证与完整打包：

```powershell
./scripts/verify.ps1
./scripts/package.ps1 -Version v0.1.0
```

打包脚本同时生成双架构运行 ZIP 和 `GeoPulse-v0.1.0-source.zip` 完整源码包，均位于 `dist/`。

## 目录结构

```text
GeoPulse/
├── cmd/geopulse/main_windows.go     # 入口、配置、诊断和生命周期
├── internal/
│   ├── app/                        # 并发调度、定位限流、网络代次
│   ├── config/                     # 仅持久化显示间隔
│   ├── ipinfo/                     # WinHTTP 文本查询及国家/IP 解析
│   ├── monitor/                    # 连通性状态机与速率计算
│   ├── network/                    # ICMP、NIC 计数、系统网络事件
│   ├── startup/                    # 当前用户注册表自启开关
│   ├── ui/                         # 隐藏窗口、托盘、动态图标、菜单
│   └── winapi/                     # 系统 DLL、单实例、资源测量
├── scripts/
│   ├── build.ps1
│   ├── package.ps1
│   └── verify.ps1
├── docs/
│   ├── ARCHITECTURE.md
│   ├── VALIDATION.md
│   ├── CHANGELOG.md
│   └── screenshots/README.md
├── .github/workflows/
│   ├── ci.yml                      # 测试、静态检查、双架构构建
│   └── release.yml                 # Tag → 交叉编译 → Release + SHA256
├── .gitattributes
├── .gitignore
├── go.mod
├── LICENSE
└── README.md
```

核心代码附中文注释；状态机、计数回滚、定位响应验证、旧网络结果隔离和 Win32 ABI 均有测试。

## 刷新与连通性判定

菜单的 1s / 2s / 5s 仅控制速率与 Tooltip 的常规显示刷新。**NIC 采样和心跳始终为 1s**；状态切换会立即推送到 UI，因此 5s 显示不会延迟断网告警。

| 场景 | 上行证据 | 下行 / 状态 |
| --- | --- | --- |
| 收到成功 Echo Reply | 已验证往返到探针目标 | 正常，显示 RTT |
| 无出口路由、网卡断开、本地 API 发送错误 | 本地发送错误 | 按连续失败计数告警 |
| Echo 超时、收到不可达或其他 ICMP 错误 | 未确认 | 第一次黄色，连续第二次红色 |

NIC 总发送量不证明公网可达；Ping 无响应也不能单独定位为上行或下行物理故障。禁止 ICMP 的企业网络、探针服务不可达或 IPv6-only 网络可能触发告警；这里的红色表示**探针连续未验证公网连通性**。

定位访问 `https://www.cloudflare.com/cdn-cgi/trace`，失败才尝试 `https://cloudflare.com/cdn-cgi/trace`，一次刷新最多两次有界请求。跟随当前用户的系统代理和 TUN 路由；每次重新建立连接，保留证书验证，禁用重定向、cookie 和 keep-alive，响应上限 16 KiB。仅解析公网 IP 与 ISO 两位国家代码，不保存城市、坐标或历史 IP。

普通自动请求在 30s 冷却内合并并延迟执行；系统确认的网络变化和手动刷新采用 2s 合并限制，可在 ICMP 不可达时尝试查询。TUN 节点切换不一定产生网卡通知，因此每秒只读取本地配置和代理内核的连接元数据，观察到变化后请求复核；没有变化信号时，每 30s 复核一次。普通模式仍为 15 分钟保底。真实网络变化或手动刷新会清除旧位置，迟到结果不会覆盖新请求；普通代理建连不会清空有效位置。

流量统计是当前探针路由出口网卡的总计数（含其他应用和局域网流量），不对物理网卡与 VPN 接口重复求和。定位跟随系统代理与 VPN/TUN 路由；仅在浏览器扩展内设置的代理无法被系统程序读取。ICMP 心跳验证系统 IPv4 路由，HTTP 代理不转发 ICMP，两者可能使用不同出口。定位可解析 IPv4 / IPv6 文本。

## 本地诊断

诊断模式不显示托盘，定时退出并输出 JSON；GUI 模式也可设置定时退出以进行资源验证：

```powershell
New-Item -ItemType Directory -Force .artifacts | Out-Null
Start-Process ./dist/GeoPulse-windows-amd64.exe -ArgumentList '--diagnose=15s --output .artifacts/diagnose.json' -Wait
Start-Process ./dist/GeoPulse-windows-amd64.exe -ArgumentList '--run-for=60s --output .artifacts/tray.json' -Wait
Get-Content .artifacts/tray.json
```

替换正在运行的版本前可执行 `./dist/GeoPulse-windows-amd64.exe --quit`，让原实例正常退出后再复制新 EXE。

诊断包含公网 IP，`.artifacts/` 默认忽略。`memory_with_tray` 在托盘仍存活时取样；`memory` 在退出托盘后取样。较新 Windows 额外返回 `private_working_set_bytes`；旧系统不支持时省略，不能解读为零。

## 创建 GitHub 仓库并推送

先在 GitHub 新建 **GeoPulse 公开空仓库**，不勾选 README、License 或 `.gitignore`（本工程已包含），然后在工程根目录逐条执行：

```powershell
git init -b main
git config user.name "你的 GitHub 用户名"
git config user.email "你的 GitHub 提交邮箱或 noreply 邮箱"
git add .
git diff --cached --stat
git commit -m "feat: add GeoPulse lightweight Windows tray monitor"
git remote add origin https://github.com/hrthas-king/GeoPulse.git
git push -u origin main
```

也可使用已登录的 GitHub CLI 一步创建并推送：

```powershell
gh auth login
gh repo create GeoPulse --public --source=. --remote=origin --push
```

如果不是 `hrthas-king` 账号，请替换 remote、README 徽标 / 链接，并修改 `go.mod` 和源码中的模块导入路径。

## 自动 Release

确认 CI 通过后创建版本 Tag：

```powershell
git tag v0.1.0
git push origin v0.1.0
```

`.github/workflows/release.yml` 先在 Windows 测试，再在 Linux 以 `CGO_ENABLED=0` 交叉编译 Windows amd64 / arm64，生成 ZIP、`SHA256SUMS.txt`，最后用 GitHub 自带 `GITHUB_TOKEN` 发布 Release，无需手动保存发布密钥。只有发布 job 有 `contents: write` 权限。手动运行工作流只生成构建附件，不发布无 Tag 的 Release。

## 许可

[MIT License](LICENSE)。
