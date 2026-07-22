# fundpeek

`fundpeek` 是一个在终端查看基金持仓和估值的 TUI 工具。它从养基宝、小倍养基同步账户和持仓，合并后保存为本地 portfolio 快照。终端里可以查看基金实时估值、估算收益和股票持仓明细，也能管理自选股、查看行情和分时图。如果还用 [基估宝](https://hzm0321.github.io/real-time-fund)，可以运行 `fundpeek push real`，把本地 portfolio 推送到基估宝云端配置。

## 功能

- 登录养基宝或小倍养基，凭据只保存在本机。
- 同步账户、基金代码、基金名称、份额、成本净值和持仓金额。
- 合并多个来源的持仓，生成供 TUI 和 JSON 使用的本地 portfolio 快照。
- 在 TUI 中查看基金实时估值、估算收益汇总和股票持仓明细。
- 管理本地自选股，查看当日涨幅、最新价和分时图。
- 用 JSON 命令查询股票搜索结果、单股行情、单股分时和自选股行情，方便脚本或大模型读取。
- 手动把本地 portfolio 推送到基估宝云端配置。

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

1. 登录养基宝或小倍养基，至少登录一个来源：

```sh
fundpeek auth yjb
fundpeek auth xb
```

养基宝登录时会在终端显示二维码。小倍养基登录需要手机号和短信验证码。

2. 查看认证状态：

```sh
fundpeek status
```

3. 刷新本地持仓数据：

```sh
fundpeek sync
```

也可以只同步单个来源：

```sh
fundpeek sync yjb
fundpeek sync xb
```

同步完成后，命令会显示本次处理的账户、基金、分组和持仓数量，并更新本地 portfolio 快照。

4. 打开 TUI 查看估值：

```sh
fundpeek tui
```

基金列表会显示基金名称、估值涨幅、最新净值涨幅、估算收益和汇总。当天估值不可用时，会根据最新净值和历史净值估算收益。选中基金后按 Enter 可查看股票持仓明细。

按 `Tab` 可切换到自选股列表，按 `a` 输入股票代码或名称添加自选股，按 `d` 删除当前选中的自选股。列表会显示交易日涨幅和最新价，选中股票后按 Enter 可查看终端字符分时图。

5. 输出基金 JSON 给脚本或大模型分析：

```sh
fundpeek json
```

这条命令会读取本地 portfolio 快照并刷新基金行情。输出包括基金列表、份额、来源持仓金额、成本金额、估算市值、最新净值市值、估值涨幅、估值口径的当日收益、最新净值涨幅和汇总。个别基金行情刷新失败时，结果中仍会保留该基金，原因记录在 `errors`。

6. 查询股票 JSON 给脚本或大模型分析：

```sh
fundpeek stock search 茅台
fundpeek stock quote 600519
fundpeek stock minute 600519
fundpeek stock list
```

`stock search` 用于搜索 A 股候选项，`stock quote` 输出单只股票的最新价和涨跌幅，`stock minute` 输出单只 A 股的分时数据，`stock list` 读取本地自选股并刷新行情。这些查询会输出结构稳定的 JSON；失败时仍会保留股票标识，原因记录在 `errors`。

7. 如果还想用基估宝 GUI 查看，登录基估宝并推送本地数据：

```sh
fundpeek auth real
fundpeek push real
```

按提示输入邮箱，并填写邮件中的 OTP 验证码。

## 常用命令

```sh
fundpeek tui
fundpeek watch list
fundpeek watch add 600519
fundpeek watch remove 600519
fundpeek stock search 茅台
fundpeek stock quote 600519
fundpeek stock minute 600519
fundpeek stock list
fundpeek json
fundpeek sync
fundpeek sync yjb
fundpeek sync xb
fundpeek push real
fundpeek logout real
fundpeek logout yjb
fundpeek logout xb
```

- `tui`：启动交互式终端界面。
- `watch`：管理本地自选股，支持 `list`、`add`、`remove`。
- `stock`：查询股票搜索结果、单股行情、单股分时或本地自选股行情，输出 JSON。
- `json`：输出基金持仓和行情 JSON，供脚本或大模型读取。
- `sync`：刷新本地 portfolio 快照；不指定来源时，刷新所有已授权来源。
- `push real`：把本地 portfolio 快照推送到基估宝云端配置。
- `logout`：删除指定来源的本地凭据。

常用来源别名：

- `real` 可写作 `r`。
- `yangjibao` 可写作 `yjb` 或 `yj`。
- `xiaobei` 可写作 `xb` 或 `xbyj`。
- `sync all` 可写作 `sync a`。

TUI 快捷键：

- `↑` / `↓` 或 `k` / `j`：移动选择。
- `Tab`：在基金列表和自选股列表间切换。
- `Enter` / `→`：在基金列表进入当前基金的股票持仓明细；在自选股列表进入当前股票分时详情。
- `Esc` / `←` / `Backspace`：从基金明细或自选股详情返回列表；在列表页退出。
- `a`：在自选股列表添加股票。
- `d`：在自选股列表删除当前股票。
- `r`：刷新当前页面行情数据。
- `R`：基金列表重新拉取已授权来源的持仓并刷新行情；基金明细页重新拉取持仓明细和股票行情；自选股列表强制刷新股票行情和分时。
- `q`：退出。
- `Ctrl+C`：退出。

## 数据来源说明

养基宝和小倍养基用于同步本地持仓。`sync` 只更新本地 portfolio 快照；只有显式运行 `push real`，本地 portfolio 才会写入基估宝云端配置。

TUI 和 JSON 使用天天基金估值接口与东方财富基金净值数据。基金股票持仓来自东方财富基金 F10，股票实时行情来自腾讯行情接口。

自选股名称搜索使用东方财富搜索接口，行情和分时来自腾讯行情接口。基金股票持仓只展示最近 6 个月内的持仓报告。

## 开发

```sh
make test
make vet
make build
make verify
```

提交前可运行 `make verify`，一次完成测试、静态检查和构建。
