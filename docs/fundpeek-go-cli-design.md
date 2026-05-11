# fundpeek Go CLI 同步方案

## 背景

`fundpeek` 设计为一个独立 Go CLI 工具，用来把养基宝、小倍养基中的账户和持仓数据同步到 `real-time-fund` 项目的 Supabase `user_configs` 中。

该工具不修改 `real-time-fund` 前端代码，也不修改 `FundVal-Live` 后端代码。它复用 `FundVal-Live` 中已经验证过的三方接口流程，并复用 Proxyman #152 抓到的 `real-time-fund` 云端写入方式。

## 目标

- 使用 Go 实现一个 CLI 工具。
- HTTP 请求统一使用 `resty`。
- 支持登录 real/Supabase。
- 支持养基宝扫码登录并读取账户、持仓。
- 支持小倍养基短信登录并读取账户、持仓。
- 将三方数据归一化后合并到 `real-time-fund` 的 `user_configs.data`。
- 写入前自动备份，支持恢复。
- 本地保存 token，不把三方 token 写入 Supabase。

## 不做的事

- 不做 Web UI。
- 不做常驻后台服务。
- 不改 `real-time-fund` 项目。
- 不改 `FundVal-Live` 项目。
- 不把养基宝、小倍养基 token 写入 Supabase。
- v1 不生成 `fundDailyEarnings` 历史收益。
- v1 不做复杂冲突 UI，只输出同步报告。

## 总体流程

```text
          +----------------+
          |  fundpeek CLI  |
          +-------+--------+
                  |
    +-------------+-------------+
    |                           |
养基宝 QR 登录              小倍养基短信登录
拉 accounts/holdings       拉 accounts/holdings
    |                           |
    +-------------+-------------+
                  |
            归一化 NormalizedHolding
                  |
          读取 real user_configs.data
                  |
             自动生成备份
                  |
          合并 funds/groups/groupHoldings
                  |
       Supabase user_configs full upsert
```

## CLI 命令设计

```text
fundpeek auth real              登录 real/Supabase，邮箱 OTP
fundpeek auth yangjibao         养基宝扫码登录
fundpeek auth xiaobei           小倍养基短信登录
fundpeek status                 查看 real、养基宝、小倍养基登录态
fundpeek sync yangjibao         只同步养基宝
fundpeek sync xiaobei           只同步小倍养基
fundpeek sync all               同步养基宝和小倍养基
fundpeek backup                 备份当前 real 云端 data
fundpeek restore <backup-file>  用备份恢复 real 云端 data
fundpeek logout <source>        删除本地 token
```

## Go 包边界

| 包 | 职责 |
|---|---|
| `cmd/fundpeek` | CLI 入口、参数解析、命令路由 |
| `internal/httpclient` | resty client 初始化、超时、重试、日志脱敏 |
| `internal/real` | Supabase OTP 登录、refresh、读取和写入 `user_configs` |
| `internal/sources/yangjibao` | 养基宝签名、二维码登录、账户、持仓 |
| `internal/sources/xiaobei` | 小倍养基短信登录、JWT 解析 `unionId`、账户、持仓 |
| `internal/normalize` | 三方数据转统一模型 |
| `internal/merge` | 统一模型合并到 real 数据结构 |
| `internal/credential` | 本地 token 保存、读取、删除 |
| `internal/backup` | 同步前备份和恢复 |

这个设计会超过 8 个文件，但边界清楚，后续接第二个数据源、换登录方式、调整 real 写入格式时不会互相牵连。

## resty 使用策略

每个外部来源使用独立的 `resty.Client`：

- `real.Client`
- `yangjibao.Client`
- `xiaobei.Client`

统一设置：

- 请求超时。
- 有限次数重试。
- 明确的 `User-Agent`。
- 错误响应包装。
- 日志中隐藏 `Authorization`、`access_token`、`refresh_token`、三方 token。

Supabase 不引 SDK，直接使用 `resty` 调 REST API。

## real/Supabase 写入方式

Proxyman #152 已验证 `real-time-fund` 的全量写入方式：

```text
POST https://mouvsqlmgymsaxikvqsh.supabase.co/rest/v1/user_configs?on_conflict=user_id
Authorization: Bearer <access_token>
apikey: <publishable key>
prefer: resolution=merge-duplicates
Content-Type: application/json
```

请求体结构：

```json
{
  "user_id": "<supabase-user-id>",
  "data": {
    "funds": [],
    "groups": [],
    "groupHoldings": {},
    "_syncMeta": {
      "deviceId": "<fundpeek-device-id>",
      "at": "<iso-time>"
    }
  },
  "updated_at": "<iso-time>",
  "last_device_id": "<fundpeek-device-id>"
}
```

v1 推荐使用 full upsert，不依赖 Supabase RPC。原因是 #152 已经验证 full upsert 成功，而 RPC 在不同数据库部署中可能不存在或签名不同。

## token 保留方案

token 只保存在本机，不写入 `real-time-fund` 的 Supabase `user_configs.data`。

| 来源 | 保存内容 | 用途 |
|---|---|---|
| real/Supabase | `user_id`、`access_token`、`refresh_token`、`expires_at` | 读取和写入 `user_configs` |
| 养基宝 | `token` | 请求 `/user_account`、`/fund_hold` |
| 小倍养基 | `accessToken`、`unionId` | 请求小倍养基接口 |

推荐保存位置：

- macOS：Keychain。
- Linux：Secret Service/libsecret。
- Windows：Credential Manager。

如果第一版不接系统凭证仓库，可以使用本地加密文件作为过渡方案，但文件里必须只保存 token，不保存同步数据快照。

## 养基宝流程

接口来源参考 `FundVal-Live/backend/api/sources/yangjibao.py`。

登录流程：

```text
GET /qr_code
返回 qr_id、qr_url
用户扫码
轮询 GET /qr_code_state/{qr_id}
状态 confirmed 后拿 token
保存 token
```

数据流程：

```text
GET /user_account
得到账户列表

对每个 account:
GET /fund_hold?account_id=<id>
得到该账户持仓
```

签名规则：

```text
md5(pathname + path_without_query + token + timestamp + SECRET)
```

请求头包含：

```text
Request-Time
Request-Sign
Content-Type: application/json
Authorization: <token>
```

养基宝持仓字段映射：

| 养基宝字段 | 归一化字段 |
|---|---|
| `fund_code` | `fundCode` |
| `fund_name` / `short_name` | `fundName` |
| `hold_share` | `share` |
| `hold_cost` | `costNav` |
| `money` | `amount` |
| `hold_day` | `operationDate` |
| `account_id` | `externalAccountId` |

## 小倍养基流程

接口来源参考 `FundVal-Live/backend/api/sources/xiaobeiyangji.py`。

登录流程：

```text
POST /yangji-api/api/send-sms
body: phoneNumber, isBind=false, version, clientType=APP

POST /yangji-api/api/login/phone
body: phone, code, clientType=PHONE, version

返回 accessToken
从 JWT payload 解析 unionId
保存 accessToken 和 unionId
```

数据流程：

```text
POST /yangji-api/api/get-account-list
得到账户列表

POST /yangji-api/api/get-hold-list
得到持仓列表

POST /yangji-api/api/get-optional-change-nav
按基金代码获取 nav/valuation
```

小倍养基持仓字段映射：

| 小倍养基字段 | 归一化字段 |
|---|---|
| 基金代码字段 | `fundCode` |
| 基金名称字段 | `fundName` |
| `money` | `amount` |
| `earnings` | 用于反推 `costNav` |
| `accountId` | `externalAccountId` |
| `nav` / `valuation` | 当前净值，用于反推份额 |
| `money / nav` | `share` |
| `(money - earnings) / share` | `costNav` |

注意：小倍养基的 `share` 在 FundVal-Live 中是通过 `money / nav` 反推出来的，可信度低于养基宝直接返回的份额。同步报告里需要标记这一点。小倍养基的持有收益不作为 real 独立字段写入，而是用 `earnings` 反推出成本价，写入 `groupHoldings[*][fundCode].cost`，让 real 按自己的公式计算持有收益。

## 归一化模型

三方数据先统一成账户和持仓，再合并到 real。

```text
NormalizedAccount
- source: yangjibao | xiaobei
- externalAccountId
- name

NormalizedHolding
- source
- externalAccountId
- fundCode
- fundName
- share
- costNav
- amount
- operationDate
- raw
```

`raw` 用于调试和同步报告，不写入 real 云端数据。

## real 数据映射

v1 推荐只写：

- `funds`
- `groups`
- `groupHoldings`
- `_syncMeta`

可选写：

- `transactions`

不默认写：

- `holdings`
- `fundDailyEarnings`

原因：

- `holdings` 是 real 的全局持仓，容易和用户手动录入混在一起。
- 三方账户天然对应 real 的 `groups`。
- `fundDailyEarnings` 是历史收益序列，不应该从当前持仓直接伪造。

### groups

每个三方账户映射成一个 real 分组：

```text
招商银行
支付宝
默认账户
账户A
```

group id 使用稳定前缀：

```text
import_yangjibao_<accountId>
import_xiaobei_<accountId>
```

如果小倍养基没有账户 ID：

```text
import_xiaobei_default
```

分组展示名默认不带来源前缀，减少 real 界面里的噪音；来源只保留在 `group.id` 中，用于下次同步时识别和替换本工具创建的分组。

如果账户下没有任何有效基金代码，则不创建对应分组，避免在 real 中出现空分组。

### groupHoldings

映射格式：

```text
groupHoldings[groupId][fundCode] = {
  share: <份额>,
  cost: <单位成本>
}
```

### funds

按 `code` 合并基金：

- 如果 real 已存在该基金，保留 real 中更丰富的字段。
- 如果 real 不存在，新增最小基金对象。

最小基金对象：

```text
code
name
addedAt
```

如果三方接口能提供净值日期、净值或估值，可以补充：

```text
dwjz
jzrq
gsz
gztime
```

### transactions

v1 可以不写交易流水。若要写，必须使用稳定 ID，避免重复导入。

推荐 ID：

```text
import:<source>:<accountId>:<fundCode>:<operationDate>
```

交易类型统一为买入：

```text
type: buy
share
amount
price: costNav
date: operationDate
groupId
isHistoryOnly: true
```

## 同步模式

### 默认模式：merge

- 只更新本工具负责的导入分组。
- 不删除用户手动创建的分组。
- 不覆盖非导入基金的持仓。
- 重复执行结果应保持稳定。

### 覆盖模式：overwrite-source

- 删除指定来源生成的 group。
- 删除指定来源生成的 groupHoldings。
- 可选删除指定来源生成的 transactions。
- 重新按当前三方数据生成。

### 不建议模式：overwrite-all

不建议提供“覆盖全部 real 数据”能力。风险过高，和同步器职责不匹配。

## 备份和回滚

每次写入 Supabase 前必须执行：

```text
GET user_configs
保存完整 data 到本地 backup 文件
再执行 upsert
```

备份文件命名：

```text
backups/real-user-config-<user_id>-<timestamp>.json
```

恢复流程：

```text
读取 backup 文件
校验 user_id
POST full upsert 回 Supabase
```

回滚不需要三方 token，只需要 real/Supabase 登录态。

## 配置

配置文件只保存非敏感信息：

```text
supabase_url
supabase_anon_key
device_id
backup_dir
default_sync_mode
```

敏感信息全部进本地凭证仓库：

```text
real access_token
real refresh_token
yangjibao token
xiaobei accessToken
xiaobei unionId
```

## 错误处理

### real token 过期

使用 refresh token 刷新 session。刷新失败则要求重新执行：

```text
fundpeek auth real
```

### 养基宝 token 失效

请求 `/user_account` 失败时，删除本地养基宝 token，并提示重新扫码：

```text
fundpeek auth yangjibao
```

### 小倍养基 token 失效

JWT 过期或接口返回未授权时，删除本地小倍养基 token，并提示重新短信登录：

```text
fundpeek auth xiaobei
```

### 写入 real 失败

- 不删除备份。
- 输出失败原因和备份路径。
- 不做自动重试覆盖。

## 同步报告

每次同步输出：

```text
来源
读取到的账户数量
读取到的基金数量
新增基金数量
更新分组数量
更新持仓数量
是否写入 transactions
备份文件路径
Supabase 写入结果
```

小倍养基额外输出：

```text
份额是否由 money/nav 反推
缺失 nav 的基金代码
```

## 关键决策

1. 使用 full upsert 写入 real。
   - Proxyman #152 已验证该路径成功。
   - 不依赖 Supabase RPC 是否存在。

2. 三方账户映射成 real 分组。
   - 可以保留来源账户边界。
   - 不污染 real 的全局 `holdings`。

3. token 只保存在本地。
   - real 云端只保存基金业务数据。
   - 导出 `user_configs.data` 时不会泄漏三方登录态。

4. 同步前强制备份。
   - 写错数据可以恢复。
   - 备份只依赖 real 登录态，不依赖三方 token。

5. 第一版优先实现养基宝。
   - 养基宝返回的份额、成本更直接。
   - 小倍养基涉及短信登录、JWT `unionId`、份额反推，复杂度更高。

## 最小可行版本

第一版只做：

```text
fundpeek auth real
fundpeek auth yangjibao
fundpeek status
fundpeek sync yangjibao
fundpeek backup
fundpeek restore <backup-file>
```

第二版再做：

```text
fundpeek auth xiaobei
fundpeek sync xiaobei
fundpeek sync all
```

## 风险和应对

### 三方 API 变更

影响范围限制在：

```text
internal/sources/yangjibao
internal/sources/xiaobei
```

归一化层和 real 写入层不需要跟着大改。

### 数据覆盖错误

每次同步前备份完整 `user_configs.data`，并且导入 group 使用稳定前缀 ID，避免误删用户手动数据。

### 10 倍数据量

v1 的瓶颈是 full upsert 体积变大。当前 real 数据体积约数十 KB，full upsert 可接受。若后续数据量明显变大，再考虑使用 partial RPC。

### token 泄漏

日志默认脱敏，Proxyman/HAR 导出前也需要确认隐藏：

```text
Authorization
access_token
refresh_token
yangjibao token
xiaobei accessToken
```

## 后续实施建议

先实现“养基宝 -> real”闭环：

```text
real 登录
养基宝扫码
读取养基宝账户和持仓
读取 real 当前 data
生成备份
合并 groups/groupHoldings/funds
full upsert 写回 Supabase
输出同步报告
```

确认这条链路稳定后，再接入小倍养基。
