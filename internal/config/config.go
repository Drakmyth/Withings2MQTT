package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	listenAddrEnv           = "WITHINGS2MQTT_LISTEN_ADDR"
	dbPathEnv               = "WITHINGS2MQTT_DB_PATH"
	keyEnv                  = "WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY"
	withingsClientIDEnv     = "WITHINGS2MQTT_WITHINGS_CLIENT_ID"
	withingsClientSecretEnv = "WITHINGS2MQTT_WITHINGS_CLIENT_SECRET"
	withingsRedirectURLEnv  = "WITHINGS2MQTT_WITHINGS_REDIRECT_URL"
)

type WithingsOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (c WithingsOAuthConfig) Validate() error {
	missing := make([]string, 0, 3)
	for _, setting := range []struct {
		name  string
		value string
	}{
		{withingsClientIDEnv, c.ClientID},
		{withingsClientSecretEnv, c.ClientSecret},
		{withingsRedirectURLEnv, c.RedirectURL},
	} {
		if setting.value == "" {
			missing = append(missing, setting.name)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("Withings account linking requires %s", strings.Join(missing, ", "))
	}
	return nil
}

type Config struct {
	ListenAddr         string
	DBPath             string
	TokenEncryptionKey []byte
	AdminAllowRemote   bool
	ShutdownTimeout    time.Duration
	WithingsOAuth      WithingsOAuthConfig
}

func FromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:      valueOrDefault(listenAddrEnv, "127.0.0.1:8080"),
		DBPath:          valueOrDefault(dbPathEnv, filepath.Join("data", "withings2mqtt.db")),
		ShutdownTimeout: 15 * time.Second,
		WithingsOAuth: WithingsOAuthConfig{
			ClientID:     strings.TrimSpace(os.Getenv(withingsClientIDEnv)),
			ClientSecret: strings.TrimSpace(os.Getenv(withingsClientSecretEnv)),
			RedirectURL:  strings.TrimSpace(os.Getenv(withingsRedirectURLEnv)),
		},
	}
	if _, _, err := net.SplitHostPort(cfg.ListenAddr); err != nil {
		return Config{}, fmt.Errorf("%s must be a host:port: %w", listenAddrEnv, err)
	}
	allowRemote, err := parseBool("WITHINGS2MQTT_ADMIN_ALLOW_REMOTE")
	if err != nil {
		return Config{}, err
	}
	cfg.AdminAllowRemote = allowRemote
	encodedKey := os.Getenv(keyEnv)
	if encodedKey == "" {
		return Config{}, fmt.Errorf("%s is required and must be a base64-encoded 32-byte key", keyEnv)
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return Config{}, fmt.Errorf("%s must be a base64-encoded 32-byte key", keyEnv)
	}
	cfg.TokenEncryptionKey = key
	return cfg, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func parseBool(name string) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
