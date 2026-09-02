# Batch B 设计定稿（YiMao 创新功能第二批）

> 本文档是 Claude Code / Codex / DeepSeek 三个模型在群里讨论收敛后的**实现前定稿**。
> 目的：在写代码前把有状态/有副作用功能的设计争议吵清楚，避免写烂。
> 状态：设计已定稿，待落地（建议实现后配合 runtime 验证再合并部署）。
>
> **历史设计说明：本文不是当前命令、功能或部署契约，部分方案并未接入运行时。当前能力以 `FEATURES.md`、`COMMANDS.md`、`.env.example` 和代码测试为准。**

---

## #1 求片「预产期」（真实档位）

**结论（三人一致）**：在**用户浏览候选列表**时计算并展示档位，不在求片提交后二次搜索（避免拖慢主流程 + 状态不一致）。

**档位规则**（O(n) 扫候选列表已有字段，零额外网络开销）：
| 档位 | 条件 |
|------|------|
| 🚀 快 | ≥2 个站点 且 ≥1 站点做种 ≥3 |
| 📦 中 | ≥1 个站点 且 做种 < 3 |
| 🐢 慢 | 候选为空 或 所有站点做种 = 0 |
| 📅 待观察 | 候选数据缺失/异常（保守兜底，不乱报） |

不伪装"精确日期"，只给档位 + 简短理由（如"2 站 3 做种"）。

**前置假设（实现前必须验证）**：用户求片前是否真的会经过"浏览候选列表"界面？若多数用户只点"一键求片"不看候选，此入口无意义——届时退回 Batch A 的处理：保留纯函数 `estimateDelivery`，不接入。

---

## #2 / #3：已在 Batch A 完成上线（集数进度条 / 拼车 +1）。

---

## #5 AI「今晚看什么」电台（定时主动私推）

**调度**：复用现有 `Scheduler`，新增 `NightRadioTask`，不新建调度器（Phase 1 刚修了 Scheduler 的并发 race，不再开第二摊）。

**开关策略——默认 opt-out**（采纳 DeepSeek，推翻初始 opt-in）：
- 默认开启；首次推送带一句"这是 YiMao 新功能'今晚看什么'，不想收回复 /radio_off"。
- 理由：opt-in 有冷启动零曝光问题（用户不知道→不开→等于没做）；opt-out + 低成本关闭是最优发现路径。
- 兜底：只推**近 14 天有交互的活跃用户** + **每用户 ≥7 天才推一次**（持久化 lastPushAt）。不活跃用户根本收不到，打扰风险被压住。

**选片可用性链（硬要求，推了点不开会一次性毁信任）**：
```
① 推荐引擎(recommendation_v2)取 top-5 TMDB ID（取5不取1，过滤后可能剩0）
② 每个 ID 查 Emby externalId=tmdb:xxx / AnyProviderIdEquals → 内部 ItemID
③ 过滤无 MediaSources / 非 Available（校验要廉价，别拉全量）
④ 剩余里随机选 1；若 0 个可播放 → 本次不推
⑤ 私聊发送，文案带推荐理由（基于观看统计，如"你最近常看悬疑+日剧，这部 8.x 分"）
```
置信不足 / 可播放候选为 0 → 宁可不发，绝不硬推垃圾片。

**前置假设**：Emby 能按 externalId 查内部 Item 并拿到 MediaSources/可播放状态。

---

## #6 求片「许愿池」（无源片众筹 + 定时重搜出源喜报）

最复杂，状态机最重。**存储：SQLite**（采纳 Codex；项目已引 modernc.org/sqlite，search_history 有先例，不增依赖；行级锁+唯一索引去重+按 state 查询，比手搓 JSON 条目级锁更稳）。

### 状态机（显式迁移表，不靠隐式时序）
```
WISHED → SEARCHING ─→ FOUND → NOTIFIED → FULFILLED
                          ↘ (用户放弃/TTL超期) → EXPIRED
               ─→ (超期 N 天) → EXPIRED
```
| 迁移 | 触发 | 副作用 |
|------|------|--------|
| WISHED→SEARCHING | 首次入调度队列 | 注册重搜 |
| SEARCHING→FOUND | 定时重搜命中 | 记录命中详情、停该条目重搜 |
| FOUND→NOTIFIED | bot 已推送 | 标记已通知、启动 TTL 倒计时 |
| NOTIFIED→FULFILLED | 用户点「立即求片」/管理员确认入库 | 发求片请求、清入库通知 @ |
| *→EXPIRED | TTL+7 天未操作 / 手动放弃 / 推送失败 | 移出调度 |

### 重搜调度（采纳 DeepSeek）
- 复用 Scheduler 注册**单个** `DailyRescan` task（不是每条目一个 timer，避免随 N 线性增长）。
- task 内轮询 `state=SEARCHING` 条目，按 `created_at % 24` 算固定时隙，只搜"时隙命中今天"的——天然错峰、无扎堆。
- 令牌桶/配额保护：站点速率配额不足当天跳过，等明天。

### 出源后：仅通知，不自动求（三人一致）
- 理由：求片流程**非幂等**，定时任务从源头自动触发，遇调度重启/时间漂移可能重复下载。人工 ack gate 根除此风险。
- 通知带「🎬 立即求片」inline 按钮（复用拼车回调机制，新 action `wish_request`），点了才走求片并置 FULFILLED。

### 去重（分层，Codex + DeepSeek）
- canonical key = TMDB/IMDb id（+ 媒体类型 + 季）。**许愿池与求片子系统必须同键**，否则 FOUND 触发求片会和用户已有求片撞出两条任务/两次下载。
- 入池时按 id 搜已有求片/订阅记录，命中 → 拒绝入池提示"已有人求过"。
- FOUND → 触发求片前再搜一遍，命中 → 直接标 FULFILLED 不触发。

### 边界 case
1. **TMDB 查不到** → 拒绝入池（去重/重搜都依赖 id），提示"没匹配到条目"。
2. **用户退群/推送失败** → 该条目置 EXPIRED 移出，不重试骚扰。
3. **命中质量差（枪版/无中字）** → 不拦（因仅通知不自动求），但通知标注"⚠️ 疑似枪版/无中字"，用户自决。
4. **容量上限** → 每人最多 ~20 条、全局 ~500 条，超出拒绝入池，防重搜任务无限膨胀。

### 入口
- `/wish <片名>`，或在搜索无结果路径给"加入许愿池"按钮（复用现有 search 无结果 handler）。

---

## 落地原则（一句话）
**非阻塞、保守估计、可播放优先、默认不打扰、所有重状态走 SQLite、自动副作用一律加人工 ack gate。**

## 实现顺序与风险
- #1（小、隔离）→ #5（中）→ #6（大、状态机+SQLite，风险最高）。
- 每个功能 `go build ./... && go vet ./... && go test ./...` 全绿才算完成；任一关键环节做不干净则保留骨架+不接入半成品。
- **建议 #6 实现后配合真实 Emby/MoviePilot 环境 runtime 验证再合并部署**（状态机/调度/去重无法仅靠编译保证正确）。

---

# 附录：三模型评审后的落地细化（v2 locked）

## 通用（跨三功能）
- **全部步长/阈值走 env config**（禁 magic number）：`WISH_RESEARCH_INTERVAL_HOURS=24`、`WISH_EXPIRE_DAYS=30`、`WISH_MIN_SEEDERS=1`、`WISH_MIN_QUALITY`(默认关)、`RADIO_MAX_PER_WEEK=2`、`RADIO_MIN_ACTIVE_DAYS=7`、`ETA_THRESHOLD_HIGH=3`。
- **结构化日志带 tag**：`[eta]` `[radio]` `[wish]`——三者都是无人值守（定时/webhook），出错没人看，要可 grep。

## #1 状态灯牌（不承诺时间）
| 灯 | 条件 | 文案 |
|----|------|------|
| ⚡ | ≥ETA_THRESHOLD_HIGH 站点做种 | 资源充足，很快到货 |
| 🔄 | 1-2 站点做种 | 已有源，需要等种 |
| 🐢 | 0 站点 | 暂无源，待补档 |
| ❓ | 候选空/数据不足 | 还在找源中…… |

## #5 细化
- 默认 opt-out 但**必须有显式关闭字段**，scheduler 读字段跳过（不靠 UI 隐藏）。
- 频控：≤2 次/周 且 间隔 ≥3 天；表 `radio_dm_log(user_id, sent_at)`，`COUNT WHERE sent_at>now-7d`。
- 近 `RADIO_MIN_ACTIVE_DAYS` 天无互动（命令/按钮）→ 不推。
- playability：`Items/{id}?Fields=Path,IsPlaceHolder,MediaSources`，校验 `!IsPlaceHolder && MediaSources非空 && Path非空`，虚拟路径跳过换下一部。

## #6 状态机落地（SQLite，全部跃迁包在同一事务）
状态：`PENDING → SEARCHING → FOUND → NOTIFIED → FULFILLED`；旁路 `EXPIRED` / `ORPHANED`。
- **坑1 TMDB 查不到**：强校验 `tmdb_id != 0`，否则拒绝入池"没找到这个片，换关键词"，不入池不占容量不重搜。
- **坑2 错峰**：`search_offset_minutes = hash(item_id) % 1440`，搜索时刻散布全天，平均探测延迟 ~12h→~1h，不增请求次数。
- **坑3 质量**：v1 不设门槛，通知里**标注**疑似枪版/无中字/分辨率，用户自决。
- **坑4 过期**：`expiry_cursor` 每天扫，超 `WISH_EXPIRE_DAYS` 无源 → 触发一次最终重搜 → 仍无则 `EXPIRED` + 私信发起人。
- **坑5 退群**：发通知前 TG `getChatMember`，不在 → `ORPHANED`，不通知不重试，管理员可 review。
- **坑6 重搜自愈**：字段 `searching_at`，调度只选 `searching_at IS NULL OR searching_at<now-interval`；搜前 `UPDATE SET searching_at=now()`，搜完清空。崩溃重启旧锁超时自动重纳入。
- **坑7 去重**：入池前查 许愿池 + 现有订阅/求片表，命中 → "这片已在求列表，出源自动通知"。唯一索引 canonical key = `tmdb_id` 优先，无则 `imdb_id`，都无拒绝入池（不用纯标题）。
- 出源后**仅通知**，inline「🎬 立即求片」→ 走**现有 request 流程 + 用户确认**（防误触自动入库）→ FULFILLED。

## 实现前置（必先跑通，Codex 最关注）
1. Emby `externalId=tmdb:xxx` 查询可用？
2. 候选列表入口能拿到可播放源 / seeders 数据？
3. 求片/订阅子系统有 TMDB/IMDb 键可去重？
