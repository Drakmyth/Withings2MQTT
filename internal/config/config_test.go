package config

import (
	"encoding/base64"
	"testing"
)

func TestFromEnvRejectsMissingEncryptionKey(t *testing.T) {
	t.Setenv(keyEnv, "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestFromEnvReadsDefaults(t *testing.T) {
	t.Setenv(keyEnv, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv(listenAddrEnv, "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
}
