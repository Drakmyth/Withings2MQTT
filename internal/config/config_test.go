package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestFromEnvRejectsMissingEncryptionKey(t *testing.T) {
	t.Setenv(keyEnv, "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestWithingsOAuthValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  WithingsOAuthConfig
		wantErr string
	}{
		{
			name: "complete",
			config: WithingsOAuthConfig{
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				RedirectURL:  "http://127.0.0.1:8080/oauth/withings/callback",
			},
		},
		{
			name:    "not configured",
			config:  WithingsOAuthConfig{},
			wantErr: withingsClientIDEnv,
		},
		{
			name: "partially configured",
			config: WithingsOAuthConfig{
				ClientID: "client-id",
			},
			wantErr: withingsClientSecretEnv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Validate() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFromEnvReadsWithingsOAuth(t *testing.T) {
	t.Setenv(keyEnv, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv(withingsClientIDEnv, "client-id")
	t.Setenv(withingsClientSecretEnv, "client-secret")
	t.Setenv(withingsRedirectURLEnv, "http://127.0.0.1:8080/oauth/withings/callback")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WithingsOAuth.ClientID != "client-id" {
		t.Fatalf("WithingsOAuth.ClientID = %q", cfg.WithingsOAuth.ClientID)
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
