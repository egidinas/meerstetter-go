package main

import "testing"

func TestParseConfigRequiresTarget(t *testing.T) {
	_, err := parseConfig([]string{"-listen", "127.0.0.1:50100"}, nil)
	if err == nil {
		t.Fatal("expected missing target to fail")
	}
}

func TestParseConfigAcceptsListenTargetAndTimeout(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-listen", "127.0.0.1:50100",
		"-target", "127.0.0.1:50000",
		"-request-timeout", "1500ms",
	}, nil)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:50100" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Server.Target != "127.0.0.1:50000" {
		t.Fatalf("Target = %q", cfg.Server.Target)
	}
	if cfg.Server.RequestTimeout.String() != "1.5s" {
		t.Fatalf("RequestTimeout = %s", cfg.Server.RequestTimeout)
	}
}
