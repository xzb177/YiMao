# 许愿热度榜（占位，未实现）

许愿众筹计数已落地：`wish_wishers(canonical_key, user_id, created_at)` join 表记录每部片有哪些用户在等。
wish_items 仍是「每个 canonical 全局一条」（状态机/重搜/调度不变），众筹人数全部来自 `wish_wishers`。

## 热度榜占位 SQL（未实现，留待后续）

```sql
-- 取等待人数 Top 10 的许愿片（按 canonical_key 聚合计数）
SELECT canonical_key, COUNT(*) AS cnt
FROM wish_wishers
GROUP BY canonical_key
ORDER BY cnt DESC
LIMIT 10;
```

> 状态：**未实现**。当前仅留 SQL 占位，零代码。后续如做「热度榜」命令/面板，
> 可在此 SQL 基础上 JOIN `wish_items` 取片名/状态展示。
> 注意 `canonical_key` 必须经 `services.CanonicalKey` 生成，与去重/计数维度严格对齐。
