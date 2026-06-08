package logx

import "testing"

func TestConfiguredLogFilePathPrecedence(t *testing.T) {
	t.Setenv(ServiceLogPathEnv, "/tmp/service.log")
	t.Setenv(LegacyLogFileEnv, "/tmp/legacy.log")
	path, source := ConfiguredLogFilePath()
	if path != "/tmp/service.log" {
		t.Fatalf("path=%q want /tmp/service.log", path)
	}
	if source != ServiceLogPathEnv {
		t.Fatalf("source=%q want %q", source, ServiceLogPathEnv)
	}
}

func TestConfiguredLogFilePathLegacyFallback(t *testing.T) {
	t.Setenv(ServiceLogPathEnv, "")
	t.Setenv(LegacyLogFileEnv, "/tmp/legacy.log")
	path, source := ConfiguredLogFilePath()
	if path != "/tmp/legacy.log" {
		t.Fatalf("path=%q want /tmp/legacy.log", path)
	}
	if source != LegacyLogFileEnv {
		t.Fatalf("source=%q want %q", source, LegacyLogFileEnv)
	}
}
