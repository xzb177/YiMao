package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
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

func TestCanonicalProductLabelsInStringLiterals(t *testing.T) {
	root := filepath.Clean("../..")
	forbidden := []string{
		"普通求片",
		"趣味求片",
		"求片大冒险",
		"我的请求",
		"我的求片",
		"本周梦魇",
		"梦魇挑战",
		"赌赢了",
		"赌局",
		"命运眷顾勇者",
		"🏠 返回主菜单",
	}
	fset := token.NewFileSet()
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			for _, phrase := range forbidden {
				if strings.Contains(value, phrase) {
					t.Errorf("%s contains legacy product copy %q", fset.Position(literal.Pos()), phrase)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("canonical copy guard did not scan production Go files")
	}
}
