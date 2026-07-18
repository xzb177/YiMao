package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalUserCopyGuard(t *testing.T) {
	root := filepath.Clean("..")
	forbidden := []string{
		"《%s}",
		"令牌: %s",
		"CallbackMsg: err.Error()",
		"下次一定赢",
		"通关率不到 10%",
		"大多数人在第2关就会倒下",
		"⬅️ 返回主菜单",
		"通关才给下载",
		"只有通关才能求片",
		"云海影视助手",
		"灵魂画像",
		"宿敌警报",
		"复仇模式激活",
		"三倍豪赌",
		"死神今天放你一马",
		"这是碾压",
		"无人能及",
		"真正的主角",
	}
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		text := string(data)
		for _, phrase := range forbidden {
			if strings.Contains(text, phrase) {
				t.Errorf("%s contains forbidden user copy %q", path, phrase)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("copy guard did not scan production Go files")
	}
}
