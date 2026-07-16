package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Drakmyth/withings2mqtt/internal/config"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(config.Config{DBPath: filepath.Join(t.TempDir(), "test.db"), TokenEncryptionKey: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}
func TestHealthz(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}
func TestAdminRejectsRemoteAndRendersLocal(t *testing.T) {
	a := newTestApp(t)
	remote := httptest.NewRequest(http.MethodGet, "/admin", nil)
	remote.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, remote)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote status = %d", w.Code)
	}
	local := httptest.NewRequest(http.MethodGet, "/admin", nil)
	local.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, local)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Linked accounts: 0") {
		t.Fatalf("unexpected admin response: %d %s", w.Code, w.Body.String())
	}
}
func TestAdminStatusHasFullPageFallback(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatal("status fallback was not a full page")
	}
}

func TestStartWithingsLinkRequiresCompleteConfiguration(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodGet, "/admin/withings/link", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "WITHINGS2MQTT_WITHINGS_CLIENT_ID") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}

func TestStartWithingsLinkRedirectsWithRequiredParameters(t *testing.T) {
	a := newTestApp(t)
	a.cfg.WithingsOAuth = config.WithingsOAuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://127.0.0.1:8080/oauth/withings/callback",
	}
	r := httptest.NewRequest(http.MethodGet, "/admin/withings/link", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme+"://"+location.Host+location.Path != withingsAuthorizeURL {
		t.Fatalf("redirect URL = %q", location.String())
	}
	want := map[string]string{
		"response_type": "code",
		"client_id":     "client-id",
		"redirect_uri":  a.cfg.WithingsOAuth.RedirectURL,
		"scope":         "user.metrics,user.activity",
	}
	for name, value := range want {
		if got := location.Query().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if state := location.Query().Get("state"); state == "" {
		t.Fatal("redirect omitted OAuth state")
	}
}

func TestOAuthStateIsOneTimeAndExpires(t *testing.T) {
	a := newTestApp(t)
	state, err := a.newOAuthState()
	if err != nil {
		t.Fatal(err)
	}
	if !a.consumeOAuthState(state) {
		t.Fatal("fresh state was rejected")
	}
	if a.consumeOAuthState(state) {
		t.Fatal("reused state was accepted")
	}
	expired, err := a.newOAuthState()
	if err != nil {
		t.Fatal(err)
	}
	a.statesMu.Lock()
	a.states[expired] = oauthState{expiresAt: time.Now().Add(-time.Second)}
	a.statesMu.Unlock()
	if a.consumeOAuthState(expired) {
		t.Fatal("expired state was accepted")
	}
	if a.consumeOAuthState("unknown") {
		t.Fatal("unknown state was accepted")
	}
}

func TestWithingsCallbackCreatesLinkedAccount(t *testing.T) {
	a := newTestApp(t)
	a.cfg.WithingsOAuth = config.WithingsOAuthConfig{ClientID: "client-id", ClientSecret: "secret", RedirectURL: "http://localhost/oauth/withings/callback"}
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("code") != "authorization-code" || r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":0,"body":{"userid":"user-1","access_token":"access","refresh_token":"refresh","expires_in":3600}}`))
	}))
	defer tokenServer.Close()
	a.tokenURL = tokenServer.URL
	state, err := a.newOAuthState()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/oauth/withings/callback?code=authorization-code&state="+url.QueryEscape(state), nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("response: %d %s", w.Code, w.Body.String())
	}
	count, err := a.store.LinkedUserCount(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("linked account count = %d, error = %v", count, err)
	}
}

func TestOAuthTokensAreEncryptedBeforeStorage(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveLinkedUser(context.Background(), "user-1", "access-secret", "refresh-secret", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := a.store.RawAccessToken(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "access-secret") {
		t.Fatal("access token is stored as plaintext")
	}
}

func TestHTMXIsServedLocally(t *testing.T) {
	a := newTestApp(t)
	r := httptest.NewRequest(http.MethodGet, "/static/htmx-2.0.4.min.js", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "version:\"2.0.4\"") {
		t.Fatalf("unexpected htmx response: %d", w.Code)
	}
}
