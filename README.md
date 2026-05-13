# fundpeek

`fundpeek` 是一个来源驱动的基金持仓 TUI 工具。它从养基宝、小倍养基获取基金账户和持仓信息，保存为本地 portfolio 快照，并在终端里查看基金实时估值、当日收益和基金股票持仓明细。如果还想用 [基估宝](https://hzm0321.github.io/real-time-fund) 的 GUI 面板查看同一份数据，可以显式执行 `fundpeek push real` 推送到基估宝云端配置。

## 功能

- 登录养基宝、小倍养基账号，并把凭据保存在本机私有配置目录。
- 从养基宝或小倍养基拉取账户、基金代码、基金名称、份额、成本净值、金额等持仓信息。
- 将多来源持仓合并成本地 portfolio 快照，供 TUI 使用。
- 通过 TUI 查看本地持仓、基金实时估值、当日收益汇总和基金股票持仓明细。
- 可选登录基估宝，并把本地 portfolio 推送到基估宝云端配置。

## 安装

需要 Go 1.25 或更高版本。

```sh
go install github.com/icpd/fundpeek/cmd/fundpeek@latest
```

也可以在仓库内构建本地二进制：

```sh
make build
./fundpeek --help
```

## 快速开始

1. 登录基金来源。至少登录一个来源：

```sh
fundpeek auth yjb
fundpeek auth xb
```

养基宝会在终端显示二维码；小倍养基会按提示输入手机号和短信验证码。

2. 查看认证状态：

```sh
fundpeek status
```

3. 刷新本地 TUI 持仓数据：

```sh
fundpeek sync
```

也可以只同步单个来源：

```sh
fundpeek sync yjb
fundpeek sync xb
```

同步成功后会输出本次处理的账户、基金、分组和持仓数量，并更新本地 portfolio 快照。

4. 打开 TUI 查看估值：

```sh
fundpeek tui
```

列表页会显示基金名称、估值涨幅、当日收益、最新净值涨幅和汇总。当天估值不可用时，会回退到最新净值和历史净值计算当日收益。选中基金后按 Enter 可查看股票持仓明细。

5. 如果还想在基估宝 GUI 面板中查看，登录基估宝并推送本地数据：

```sh
fundpeek auth real
fundpeek push real
```

按提示输入邮箱，并填写邮件中的 OTP 验证码。

## 常用命令

```sh
fundpeek tui
fundpeek sync
fundpeek sync yjb
fundpeek sync xb
fundpeek push real
fundpeek logout real
fundpeek logout yjb
fundpeek logout xb
```

- `tui`：启动交互式终端界面。
- `sync`：刷新本地 portfolio 快照；不带来源时默认刷新所有已授权来源。
- `push real`：把本地 portfolio 快照推送到基估宝云端配置。
- `logout`：删除指定来源的本地凭据。

常用来源别名：

- `real` 可写作 `r`。
- `yangjibao` 可写作 `yjb` 或 `yj`。
- `xiaobei` 可写作 `xb` 或 `xbyj`。
- `sync all` 可写作 `sync a`。

TUI 快捷键：

- `↑` / `↓` 或 `k` / `j`：移动选择。
- `Enter` / `→`：进入当前基金的股票持仓明细。
- `Esc` / `←` / `Backspace`：从明细页返回列表；在列表页退出。
- `r`：刷新当前页面行情数据。
- `R`：列表页重新拉取已授权来源的持仓并刷新行情；明细页重新拉取持仓明细和股票行情。
- `q`：退出。
- `Ctrl+C`：退出。

## 配置与本地文件

默认配置目录是 `~/.fundpeek`，其中包含：

- `credentials.json`：本地凭据文件。
- `device_id`：本机设备 ID。
- `cache/`：本地 portfolio、基金估值、股票持仓和股票行情缓存。

可用环境变量：

```sh
FUNDPEEK_CONFIG_DIR=/path/to/config
FUNDPEEK_DEVICE_ID=custom-device-id
FUNDPEEK_SUPABASE_URL=https://example.supabase.co
FUNDPEEK_SUPABASE_ANON_KEY=...
```

不要提交凭据、缓存或本地配置文件。

## 数据来源说明

本地持仓数据来自养基宝和小倍养基；`push real` 会把本地 portfolio 快照写入基估宝云端配置。TUI 的基金估值来自天天基金估值接口和东方财富基金净值数据，基金股票持仓来自东方财富基金 F10，股票实时行情来自腾讯行情接口。股票持仓明细只展示最近 6 个月内的基金持仓报告；过期报告不会展示。

## 开发

```sh
make test
make vet
make build
make verify
```

`make verify` 会依次运行测试、静态检查和构建，提交前建议执行。
