package services

import "testing"

func TestRichMessageEnabledDefault(t *testing.T) {
	t.Setenv("ENABLE_RICH_MESSAGE", "")
	if !richMessageEnabled() {
		t.Fatal("rich message should be enabled by default")
	}
}

func TestRichMessageEnabledFalseValues(t *testing.T) {
	for _, value := range []string{"false", "0", "no", "off", "FALSE"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ENABLE_RICH_MESSAGE", value)
			if richMessageEnabled() {
				t.Fatalf("rich message should be disabled for %q", value)
			}
		})
	}
}
