package services

import (
	"os"
	"path/filepath"
	"testing"
)

// 审计 A8：旧格式迁移选超管以前用 map 迭代的第一个 ID（随机），
// 现在必须是确定性规则：ID 最小的成为超管。
func TestLegacyMigrationRootIsSmallestID(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"admins": {"999": "后来者", "111": "服主", "555": "中间"}}`
	if err := os.WriteFile(filepath.Join(dir, "admins.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		// 每次重写旧文件再加载，模拟多次迁移：结果必须一致。
		if err := os.WriteFile(filepath.Join(dir, "admins.json"), []byte(legacy), 0600); err != nil {
			t.Fatal(err)
		}
		svc := NewAdminService(dir)
		if !svc.IsRootAdmin(111) {
			t.Fatalf("第 %d 次迁移：超管应为 ID 最小的 111", i+1)
		}
		if svc.IsRootAdmin(999) || svc.IsRootAdmin(555) {
			t.Fatalf("第 %d 次迁移：其他人不应是超管", i+1)
		}
	}
}
