package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
)

// GetPerformanceStats returns current performance metrics
func GetPerformanceStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := map[string]interface{}{
		// Memory stats
		"alloc_mb":       m.Alloc / 1024 / 1024,
		"total_alloc_mb": m.TotalAlloc / 1024 / 1024,
		"sys_mb":         m.Sys / 1024 / 1024,
		"num_gc":        m.NumGC,
		"next_gc_mb":     m.NextGC / 1024 / 1024,

		// Goroutines
		"goroutines":    runtime.NumGoroutine(),

		// HTTP Client pool
		"http_transport": map[string]interface{}{
			"max_idle_conns":        100,
			"max_idle_conns_per_host": 10,
			"idle_conn_timeout":     "90s",
		},

		// Data files
		"data_files": getDataFileStats(),
	}

	return stats
}

func getDataFileStats() map[string]int {
	files := map[string]int{
		"admins":       1,
		"analytics":    1,
		"user_quotas":   1,
		"preferences":  1,
		"engagement":   1,
		"user_mappings": 1,
		"trending_cache": 1,
	}

	// Check which files exist and are non-empty
	for file := range files {
		data, err := os.ReadFile(file + ".json")
		if err != nil || len(data) < 10 {
			files[file] = 0
		} else {
			files[file] = 1
		}
	}

	return files
}

// PrintPerformanceStats prints performance stats to log
func PrintPerformanceStats() {
	stats := GetPerformanceStats()
	log.Printf("[PERF] Memory: Alloc=%.1fMB, Sys=%.1fMB, GC=%d, Goroutines=%d",
		stats["alloc_mb"], stats["sys_mb"], stats["num_gc"], stats["goroutines"])
}

// ForceGC forces garbage collection
func ForceGC() {
	runtime.GC()
	log.Println("[PERF] Forced garbage collection")
}

// GetRuntimeInfo returns formatted runtime information
func GetRuntimeInfo() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return fmt.Sprintf(`📊 *性能统计*

🧠 内存使用
• 已分配: %.1f MB
• 系统占用: %.1f MB
• GC 次数: %d
• Goroutines: %d

⚡ 优化状态
• HTTP 连接池: ✅ 已启用
• JSON Buffer Pool: ✅ 已启用
• String Builder Pool: ✅ 已启用`,
		float64(m.Alloc)/1024/1024,
		float64(m.Sys)/1024/1024,
		m.NumGC,
		runtime.NumGoroutine(),
	)
}
