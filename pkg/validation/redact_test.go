package validation

import "testing"

func TestRedactSensitiveText(t *testing.T) {
	cases := map[string]string{
		"/link alice s3cret":       "/link [redacted]",
		"/link alice pass with sp": "/link [redacted]",
		"/LINK alice pw":           "/LINK [redacted]",
		"/link@yimao_bot alice pw": "/link@yimao_bot [redacted]",
		"/link":                    "/link",
		"/resetpw alice":           "/resetpw [redacted]",
		"/start":                   "/start",
		"/search 星际穿越":             "/search 星际穿越",
		"星际穿越":                     "星际穿越",
		"  /link bob pw  ":         "/link [redacted]",
	}
	for in, want := range cases {
		if got := RedactSensitiveText(in); got != want {
			t.Errorf("RedactSensitiveText(%q) = %q, want %q", in, got, want)
		}
	}
}
