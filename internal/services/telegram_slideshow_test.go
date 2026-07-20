package services

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestSendStructuredRichMessageSlideshowContract(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatal("sendRichMessage must not contain ephemeral receiver_user_id")
		}
		if _, ok := payload["reply_markup"]; !ok {
			t.Fatal("reply_markup must be at sendRichMessage top level")
		}
		rich := payload["rich_message"].(map[string]any)
		if len(rich) != 1 {
			t.Fatalf("InputRichMessage variants=%v, want exactly slideshow", rich)
		}
		rootBlocks := rich["blocks"].([]any)
		if len(rootBlocks) != 1 {
			t.Fatalf("root blocks=%v", rootBlocks)
		}
		slideshow := rootBlocks[0].(map[string]any)
		if slideshow["type"] != "slideshow" || len(slideshow["blocks"].([]any)) != 2 {
			t.Fatalf("slideshow=%v", slideshow)
		}
		block := slideshow["blocks"].([]any)[0].(map[string]any)
		photo := block["photo"].(map[string]any)
		if block["type"] != "photo" || photo["type"] != "photo" || photo["media"] != "https://example.com/1.jpg" {
			t.Fatalf("photo block=%v", block)
		}
		writeMessageOK(t, w)
	})
	rich := &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{
		Type: "slideshow",
		Blocks: []types.TelegramInputRichBlock{
			{Type: "photo", Photo: &types.TelegramRichPhoto{Type: "photo", Media: "https://example.com/1.jpg"}, Caption: &types.TelegramRichText{Text: "1. 一"}},
			{Type: "photo", Photo: &types.TelegramRichPhoto{Type: "photo", Media: "file-id-2"}, Caption: &types.TelegramRichText{Text: "2. 二"}},
		},
	}}}
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{{Text: "一", CallbackData: "select:id:1:type:movie"}}}}
	if _, err := client.SendStructuredRichMessage(42, rich, keyboard); err != nil {
		t.Fatal(err)
	}
}

func TestSendStructuredRichMessageRejectsMultipleVariants(t *testing.T) {
	client := NewTelegramClient("test")
	_, err := client.SendStructuredRichMessage(42, &types.TelegramInputRichMessage{
		Markdown: "x", Blocks: []types.TelegramInputRichBlock{{Type: "slideshow"}},
	}, nil)
	if err == nil {
		t.Fatal("expected exactly-one validation error")
	}
}

func TestSendStructuredRichMessageMultipartAttachments(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("content-type=%q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		var rich map[string]any
		if err := json.Unmarshal([]byte(r.FormValue("rich_message")), &rich); err != nil {
			t.Fatal(err)
		}
		media := rich["media"].([]any)[0].(map[string]any)
		if _, leaked := media["upload"]; leaked {
			t.Fatalf("upload bytes leaked into JSON: %v", media)
		}
		file, header, err := r.FormFile("card_1")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if header.Filename != "card_1.jpg" || string(data) != "jpeg-data" {
			t.Fatalf("file=%q data=%q", header.Filename, data)
		}
		writeMessageOK(t, w)
	})
	rich := &types.TelegramInputRichMessage{
		Blocks: []types.TelegramInputRichBlock{{Type: "slideshow", Blocks: []types.TelegramInputRichBlock{{Type: "photo", Photo: &types.TelegramRichPhoto{Type: "photo", Media: "attach://card_1"}}}}},
		Media:  []types.TelegramInputRichMessageMedia{{ID: "card_1", Media: types.TelegramRichPhoto{Type: "photo", Media: "attach://card_1"}, Upload: []byte("jpeg-data"), Filename: "card_1.jpg"}},
	}
	if _, err := client.SendStructuredRichMessage(42, rich, nil); err != nil {
		t.Fatal(err)
	}
}
