package dedupfile

// 持久化去重账本：把「已处理过的 key → 首次处理时间（unix 秒）」落盘为 JSON，
// 进程重启后仍能判重。被 Telegram webhook 去重（internal/bot）和 Emby webhook
// 指纹去重（internal/api）共用——横切能力只许一套实现。
//
// 语义：
//   - Load 读不到文件 / 解析失败 / 条目过期 → 返回空表 + nil 错误（fail-open：
//     宁可重复执行一次，也不能因为账本读失败而拒绝处理新消息）。
//   - Save 原子写（tmp + rename），失败返回 error，调用方只记日志（fail-open）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Load 读取账本文件，丢弃早于 ttl 的过期条目。
func Load(path string, ttl time.Duration) map[string]int64 {
	empty := map[string]int64{}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty // 文件不存在是常态（首次启动）
	}
	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return empty // 损坏的账本不拦路，fail-open
	}
	now := time.Now().Unix()
	out := make(map[string]int64, len(raw))
	for k, ts := range raw {
		if ttl <= 0 || now-ts < int64(ttl/time.Second) {
			out[k] = ts
		}
	}
	return out
}

// Save 原子落盘。调用方失败时只记日志，不影响业务（fail-open）。
func Save(path string, entries map[string]int64) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
