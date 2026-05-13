# fundpeek 主定位调整：来源驱动的基金 TUI，估基宝作为可选 GUI 同步目标

## Summary

把 `fundpeek` 的主路径从“把数据同步到估基宝”改成“从养基宝/小倍养基获取持仓，本地 TUI 查看估值”。估基宝只作为可选 GUI 面板目标，通过 `fundpeek push real` 显式推送。

这会改动 CLI、应用层数据流、TUI 刷新、文档和测试，预计超过 8 个文件；不引入新服务、新依赖或新运行时。

数据流：

```text
养基宝/小倍养基 -> sync -> 本地 portfolio_data -> tui
                                      |
                                      v
                                  push real -> 估基宝
```

## Key Changes

- `fundpeek sync [source]` 改成本地持仓刷新：
  - `fundpeek sync` 默认等同 `sync all`。
  - 支持 `sync yjb|xb|all`。
  - 不再写入估基宝。
  - 成功后更新本地 `portfolio_data` 快照。
- 新增 `fundpeek push real`：
  - 读取本地 `portfolio_data`，推送到估基宝云端配置。
  - 需要 `auth real`。
  - 保留 real 现有冲突检测。
  - 不做备份。
  - 本地无导入持仓时默认拒绝推送，提示先运行 `fundpeek sync`。
- 本地快照改为 `portfolio_data`：
  - 缓存文件仍使用现有 cache envelope：`fetched_at` + `value`。
  - `value` 保持 real-compatible data 结构：`funds`、`groups`、`groupHoldings`。
  - 不读取旧 `real_data` 作为兼容来源；旧本地数据需要手动迁移或重新执行 `fundpeek sync`。
- `sync all` 和 TUI 强制刷新采用 best-effort：
  - 至少一个来源成功就更新本地快照。
  - 未授权或失败的来源保留上次本地数据中的对应导入分组。
  - 输出/显示哪些来源成功、跳过或失败。
  - 如果没有任何来源成功，则不更新快照并返回错误。
- TUI 改为本地 portfolio 主数据：
  - 启动 TUI 不依赖 real 授权。
  - 普通刷新 `r` 刷新行情/明细行情。
  - 强制刷新 `R` 重新抓取已授权来源并更新本地 portfolio；部分失败时保留失败来源旧数据并显示警告。
- 删除备份能力：
  - 移除 `backup` / `restore` 命令。
  - 移除同步/推送前自动备份。
  - 移除 `BackupDir` 配置字段和 backup 包引用。
  - README 和 help 不再介绍备份/恢复。

## Public Interface

保留：

- `fundpeek auth real|yjb|xb`
- `fundpeek status`
- `fundpeek tui`
- `fundpeek sync`
- `fundpeek sync yjb|xb|all`
- `fundpeek logout real|yjb|xb`

新增：

- `fundpeek push real`

删除：

- `fundpeek backup`
- `fundpeek restore <backup-file> [--yes]`

## Test Plan

- CLI tests:
  - `sync` 不带参数解析为 `all`。
  - `push real` 被识别，未知 push target 报错。
  - `backup` / `restore` 不再出现在 help 中，并作为未知命令处理。
  - help 文案体现 TUI 主路径和 `push real` 可选路径。
- App tests:
  - `sync yjb` 只更新 `portfolio_data`，不调用 real update。
  - `push real` 从 `portfolio_data` 写入 real。
  - 空 portfolio 推送被拒绝。
  - `sync all` 部分来源失败时保留失败来源旧导入分组。
  - 没有任何来源成功时不更新 portfolio。
  - `portfolio_data` 不读取旧 `real_data` 回退。
- TUI tests:
  - 启动加载从 portfolio 读取，不要求 real 凭据。
  - `r` 不刷新来源持仓。
  - `R` 触发来源刷新并保留失败来源旧数据。
  - 无 portfolio 时提示先运行 `fundpeek sync`。
- Verification:
  - `make test`
  - `make vet`
  - `make build`
  - 最后运行 `make verify`

## Assumptions

- 本地 portfolio 的内部结构继续兼容估基宝 data schema，这样 `push real` 不需要新转换层。
- 删除备份是有意破坏性变更；推送安全依赖 real 现有冲突检测和空数据拒绝策略。
- `push real` 只处理 fundpeek 导入分组，保留估基宝中非导入/手动分组。
- 不新增 GUI、本地 Web 面板或新的数据文件 schema。
