package bot

import (
	"os"
	"strings"
	"testing"
)

func TestUserResponsesDoNotExposeInternalErrors(t *testing.T) {
	files := []string{"command.go", "poll.go", "webhook.go", "../api/router.go"}
	forbidden := []string{
		`"❌ 解绑失败："+err.Error()`,
		`"❌ 绑定失败："+err.Error()`,
		`"❌ 密码重置失败："+err.Error()`,
		`"❌ 画像生成失败："+err.Error()`,
		`"❌ 解说生成失败: "+err.Error()`,
		`"❌ 发表影评失败: "+err.Error()`,
		`fmt.Sprintf("❌ 回复失败: %v", err)`,
		`http.Error(w, err.Error(), http.StatusInternalServerError)`,
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, pattern := range forbidden {
			if strings.Contains(text, pattern) {
				t.Errorf("%s exposes an internal error through %q", file, pattern)
			}
		}
	}
}
