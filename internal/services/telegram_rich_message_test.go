package services

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

const telegramTokenLeakSentinel = "SENTINEL_TELEGRAM_TOKEN_DO_NOT_LEAK"

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSendRichMessageNetworkErrorsDoNotLeakBotToken(t *testing.T) {
	t.Setenv("ENABLE_RICH_MESSAGE", "true")

	tests := []struct {
		name string
		send func(*TelegramClient) error
	}{
		{
			name: "JSON",
			send: func(client *TelegramClient) error {
				_, err := client.SendRichMessage(42, "hello", nil)
				return err
			},
		},
		{
			name: "multipart upload",
			send: func(client *TelegramClient) error {
				richMessage := &types.TelegramInputRichMessage{
					Markdown: "hello",
					Media: []types.TelegramInputRichMessageMedia{{
						ID:       "poster",
						Media:    types.TelegramRichPhoto{Type: "photo", Media: "attach://poster"},
						Upload:   []byte("image bytes"),
						Filename: "poster.jpg",
					}},
				}
				_, err := client.SendStructuredRichMessage(42, richMessage, nil)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialErr := errors.New("dial failed")
			client := NewTelegramClient(telegramTokenLeakSentinel)
			client.httpClient = &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, &url.Error{Op: req.Method, URL: req.URL.String(), Err: dialErr}
			})}

			err := test.send(client)
			if err == nil {
				t.Fatal("expected network error")
			}
			if strings.Contains(err.Error(), telegramTokenLeakSentinel) {
				t.Fatalf("returned error leaked bot token: %v", err)
			}
			if !errors.Is(err, dialErr) {
				t.Fatalf("returned error does not unwrap to transport cause: %v", err)
			}
		})
	}
}
