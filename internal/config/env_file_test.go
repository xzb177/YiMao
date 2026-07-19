package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileParsesWithoutShellExpansion(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nYIMAO_TEST_PLAIN=value\nYIMAO_TEST_JSON='{\"key\":\"description\"}'\nYIMAO_TEST_LITERAL=$(touch /tmp/must-not-exist)\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"YIMAO_TEST_PLAIN", "YIMAO_TEST_JSON", "YIMAO_TEST_LITERAL"} {
		t.Setenv(key, "")
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("YIMAO_TEST_PLAIN"); got != "value" {
		t.Fatalf("plain value = %q", got)
	}
	if got := os.Getenv("YIMAO_TEST_JSON"); got != `{"key":"description"}` {
		t.Fatalf("JSON value = %q", got)
	}
	if got := os.Getenv("YIMAO_TEST_LITERAL"); got != "$(touch /tmp/must-not-exist)" {
		t.Fatalf("literal value = %q", got)
	}
}

func TestLoadEnvFileRejectsInvalidLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("bad-key=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(path); err == nil {
		t.Fatal("expected invalid key error")
	}
}
