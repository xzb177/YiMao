package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadMonitorURLsRequireExplicitConfigAndAllowOverride(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", strings.Repeat("x", 30))
	t.Setenv("MOVIEPILOT_URL", "http://moviepilot.test")
	t.Setenv("MOVIEPILOT_API_KEY", strings.Repeat("k", 10))
	t.Setenv("ENABLE_API_AUTH", "false")
	t.Setenv("DATA_DIR", t.TempDir())

	t.Setenv("MONITOR_OVERVIEW_URL", "")
	t.Setenv("MONITOR_QBIT_MAINDATA_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.MonitorOverviewURL; got != "" {
		t.Fatalf("default MonitorOverviewURL=%q want empty", got)
	}
	if got := configStringField(t, cfg, "MonitorQBitMainDataURL"); got != "" {
		t.Fatalf("default MonitorQBitMainDataURL=%q want empty", got)
	}

	t.Setenv("MONITOR_OVERVIEW_URL", "http://monitor.test/private-overview")
	t.Setenv("MONITOR_QBIT_MAINDATA_URL", "http://qbit.test/private-maindata")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.MonitorOverviewURL, "http://monitor.test/private-overview"; got != want {
		t.Fatalf("configured MonitorOverviewURL=%q want %q", got, want)
	}
	if got, want := configStringField(t, cfg, "MonitorQBitMainDataURL"), "http://qbit.test/private-maindata"; got != want {
		t.Fatalf("configured MonitorQBitMainDataURL=%q want %q", got, want)
	}
}

func configStringField(t *testing.T, cfg *Config, name string) string {
	t.Helper()
	field := reflect.ValueOf(cfg).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("Config.%s is missing", name)
	}
	return field.String()
}
