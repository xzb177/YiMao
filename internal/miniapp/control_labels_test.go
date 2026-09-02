package miniapp

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var (
	literalButtonLabelRE = regexp.MustCompile(`>([一-龥]+)</button>`)
	taskEntryLabelRE     = regexp.MustCompile(`class="task-entry"[\s\S]*?<b>([一-龥]+)</b>`)
	dataControlLabelRE   = regexp.MustCompile(`text:"([一-龥]+)"`)
	tabLabelRE           = regexp.MustCompile(`tab\("[^"]+","([一-龥]+)"`)
	stageLabelRE         = regexp.MustCompile(`\["(?:all|pending|active|done|missing)","([一-龥]+)"`)
	businessBadgeLabelRE = regexp.MustCompile(`business-badge\">([一-龥]+)<`)
	resultReturnRE       = regexp.MustCompile(`function detailPrimaryAction\([\s\S]*?\nfunction`)
)

var shippedControlLexicon = map[string]bool{
	"搜索求片": true, "求片模式": true, "洗版模式": true, "开始搜索": true,
	"系统设置": true, "问题反馈": true, "观影画像": true, "申请洗版": true, "帮助说明": true, "更多功能": true,
	"刷新状态": true, "状态确认": true,
	"返回首页": true, "查看进度": true, "查看详情": true, "继续求片": true,
	"重新加载": true, "求第一部": true, "查看全部": true, "继续加载": true,
	"动态内容": true, "发现内容": true, "助手功能": true, "想看列表": true,
	"进入许愿": true, "收起进度": true, "展开进度": true,
	"撤回任务": true, "选择季度": true, "求片提交": true, "状态待确认": true,
	"继续查看": true, "确认求片": true, "确认洗版": true,
	"查看任务": true, "继续找片": true, "保留任务": true, "确认撤回": true,
	"全部内容": true, "电影内容": true, "剧集内容": true, "需要处理": true,
	"正在进行": true, "已经完成": true, "没有找到": true,
}

func addControlLabel(t *testing.T, labels map[string]bool, label string) {
	t.Helper()
	label = strings.TrimSpace(label)
	if label == "" || label == "×" {
		return
	}
	labels[label] = true
}

func extractTemplateControlLabels(t *testing.T, source string) map[string]bool {
	t.Helper()
	labels := map[string]bool{}
	for _, match := range literalButtonLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	for _, match := range taskEntryLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	for _, match := range dataControlLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	for _, match := range tabLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	for _, match := range stageLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	for _, match := range businessBadgeLabelRE.FindAllStringSubmatch(source, -1) {
		addControlLabel(t, labels, match[1])
	}
	// These are returned by helpers and then emitted inside button/span controls.
	for _, fn := range []string{"taskNext", "detailPrimaryAction"} {
		start := strings.Index(source, "function "+fn+"(")
		if start < 0 {
			t.Fatalf("missing control-label helper %s", fn)
		}
		end := strings.Index(source[start:], "\nfunction ")
		if end < 0 {
			end = len(source) - start
		}
		block := source[start : start+end]
		for _, match := range regexp.MustCompile(`return['"]([一-龥]+)['"]`).FindAllStringSubmatch(block, -1) {
			addControlLabel(t, labels, match[1])
		}
		for _, match := range regexp.MustCompile(`[?:]['"]([一-龥]+)['"]`).FindAllStringSubmatch(block, -1) {
			addControlLabel(t, labels, match[1])
		}
	}
	return labels
}

func TestMiniAppTemplateControlLabelsAreExactlyFourCJK(t *testing.T) {
	b, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	labels := extractTemplateControlLabels(t, source)
	for label := range labels {
		runes := []rune(label)
		if len(runes) != 4 {
			t.Errorf("template control label %q has %d runes", label, len(runes))
		}
		for _, r := range runes {
			if !unicode.Is(unicode.Han, r) {
				t.Errorf("template control label %q contains non-CJK rune %q", label, r)
			}
		}
		if !shippedControlLexicon[label] {
			t.Errorf("template control label %q is outside shipped lexicon", label)
		}
	}
	for _, want := range []string{"搜索求片", "求片模式", "洗版模式", "开始搜索", "返回首页", "查看进度"} {
		if !labels[want] {
			t.Errorf("missing template control label %q", want)
		}
	}
}

func TestMiniAppForbiddenShortControlLabelsAreAbsent(t *testing.T) {
	b, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	for _, label := range []string{"搜索", "求片", "洗版", "去搜索"} {
		patterns := []string{
			">" + label + "</button>",
			">" + label + "</b>",
			">" + label + "</span>",
			`text:"` + label + `"`,
			`tab("` + label + `"`,
		}
		for _, pattern := range patterns {
			if strings.Contains(source, pattern) {
				t.Errorf("forbidden standalone control label remains: %s", pattern)
			}
		}
	}
}
