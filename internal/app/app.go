package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Drakmyth/withings2mqtt/internal/config"
	crypt "github.com/Drakmyth/withings2mqtt/internal/crypto"
	"github.com/Drakmyth/withings2mqtt/internal/store"
	"github.com/Drakmyth/withings2mqtt/internal/withings"
)

//go:embed templates/admin.html static/htmx-2.0.4.min.js
var assets embed.FS

const withingsAuthorizeURL = "https://account.withings.com/oauth2_user/authorize2"
const withingsTokenURL = "https://wbsapi.withings.net/v2/oauth2"

type oauthState struct{ expiresAt time.Time }

type App struct {
	cfg        config.Config
	store      *store.Store
	cipher     *crypt.TokenCipher
	templates  *template.Template
	httpClient *http.Client
	tokenURL   string
	withings   withings.Client
	statesMu   sync.Mutex
	states     map[string]oauthState
}
type adminData struct {
	LinkedUsers []store.LinkedUser
	SyncMessage string
}

type syncResult struct {
	Message string
	Failed  bool
}

func New(cfg config.Config) (*App, error) {
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	cipher, err := crypt.NewTokenCipher(cfg.TokenEncryptionKey)
	if err != nil {
		s.Close()
		return nil, err
	}
	templates, err := template.ParseFS(assets, "templates/admin.html")
	if err != nil {
		s.Close()
		return nil, err
	}
	return &App{cfg: cfg, store: s, cipher: cipher, templates: templates, httpClient: http.DefaultClient, tokenURL: withingsTokenURL, withings: withings.Client{BaseURL: withings.DefaultBaseURL, HTTPClient: http.DefaultClient}, states: make(map[string]oauthState)}, nil
}
func (a *App) Close() error { return a.store.Close() }

// SaveLinkedUser encrypts OAuth tokens before persisting them.
func (a *App) SaveLinkedUser(ctx context.Context, id, accessToken, refreshToken string, expiry time.Time) error {
	access, err := a.cipher.Encrypt([]byte(accessToken))
	if err != nil {
		return err
	}
	refresh, err := a.cipher.Encrypt([]byte(refreshToken))
	if err != nil {
		return err
	}
	return a.store.SaveLinkedUser(ctx, store.LinkedUser{WithingsUserID: id, AccessToken: access, RefreshToken: refresh, TokenExpiry: expiry})
}

// AccessToken returns a usable token, refreshing and persisting it when expired.
func (a *App) AccessToken(ctx context.Context, id string) (string, error) {
	user, err := a.store.LinkedUser(ctx, id)
	if err != nil {
		return "", fmt.Errorf("load linked user: %w", err)
	}
	access, err := a.cipher.Decrypt(user.AccessToken)
	if err != nil {
		return "", fmt.Errorf("decrypt access token: %w", err)
	}
	if time.Now().Before(user.TokenExpiry.Add(-time.Minute)) {
		return string(access), nil
	}
	refresh, err := a.cipher.Decrypt(user.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}
	values := url.Values{"action": {"requesttoken"}, "grant_type": {"refresh_token"}, "client_id": {a.cfg.WithingsOAuth.ClientID}, "client_secret": {a.cfg.WithingsOAuth.ClientSecret}, "refresh_token": {string(refresh)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("refresh access token: %w", err)
	}
	defer resp.Body.Close()
	var token tokenResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&token) != nil || token.Status != 0 || token.Body.AccessToken == "" {
		return "", fmt.Errorf("refresh access token: invalid response")
	}
	if token.Body.RefreshToken == "" {
		token.Body.RefreshToken = string(refresh)
	}
	if err := a.SaveLinkedUser(ctx, id, token.Body.AccessToken, token.Body.RefreshToken, time.Now().Add(time.Duration(token.Body.ExpiresIn)*time.Second)); err != nil {
		return "", fmt.Errorf("persist refreshed token: %w", err)
	}
	return token.Body.AccessToken, nil
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /admin", noStore(a.admin))
	mux.HandleFunc("GET /admin/status", noStore(a.adminStatus))
	mux.HandleFunc("GET /admin/withings/link", noStore(a.startWithingsLink))
	mux.HandleFunc("GET /oauth/withings/callback", noStore(a.withingsCallback))
	mux.HandleFunc("POST /admin/withings/{id}/unlink", noStore(a.unlinkWithings))
	mux.HandleFunc("POST /admin/withings/{id}/sync/weight", noStore(a.manualWeightSync))
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return mux
}

func noStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next(w, r)
	}
}
func (a *App) startWithingsLink(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		if err := a.cfg.WithingsOAuth.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		state, err := a.newOAuthState()
		if err != nil {
			http.Error(w, "could not start account linking", http.StatusInternalServerError)
			return
		}
		params := url.Values{
			"response_type": {"code"},
			"client_id":     {a.cfg.WithingsOAuth.ClientID},
			"redirect_uri":  {a.cfg.WithingsOAuth.RedirectURL},
			"scope":         {"user.metrics,user.activity"},
			"state":         {state},
		}
		http.Redirect(w, r, withingsAuthorizeURL+"?"+params.Encode(), http.StatusFound)
	})(w, r)
}

func (a *App) newOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	a.statesMu.Lock()
	a.states[state] = oauthState{expiresAt: time.Now().Add(10 * time.Minute)}
	a.statesMu.Unlock()
	return state, nil
}

func (a *App) consumeOAuthState(state string) bool {
	a.statesMu.Lock()
	defer a.statesMu.Unlock()
	entry, ok := a.states[state]
	delete(a.states, state)
	return ok && time.Now().Before(entry.expiresAt)
}

type tokenResponse struct {
	Status int `json:"status"`
	Body   struct {
		UserID       withingsUserID `json:"userid"`
		AccessToken  string         `json:"access_token"`
		RefreshToken string         `json:"refresh_token"`
		ExpiresIn    int            `json:"expires_in"`
	} `json:"body"`
}

type withingsUserID string

func (id *withingsUserID) UnmarshalJSON(data []byte) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	switch value := value.(type) {
	case string:
		*id = withingsUserID(value)
	case json.Number:
		*id = withingsUserID(value.String())
	default:
		return fmt.Errorf("invalid Withings user ID")
	}
	return nil
}

func (a *App) withingsCallback(w http.ResponseWriter, r *http.Request) {
	if !a.consumeOAuthState(r.URL.Query().Get("state")) {
		http.Error(w, "invalid or expired OAuth state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	values := url.Values{"action": {"requesttoken"}, "grant_type": {"authorization_code"}, "client_id": {a.cfg.WithingsOAuth.ClientID}, "client_secret": {a.cfg.WithingsOAuth.ClientSecret}, "code": {code}, "redirect_uri": {a.cfg.WithingsOAuth.RedirectURL}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, a.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	var token tokenResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&token) != nil || token.Status != 0 || token.Body.UserID == "" {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	if err := a.SaveLinkedUser(r.Context(), string(token.Body.UserID), token.Body.AccessToken, token.Body.RefreshToken, time.Now().Add(time.Duration(token.Body.ExpiresIn)*time.Second)); err != nil {
		http.Error(w, "could not save linked account", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (a *App) localAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.AdminAllowRemote {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil || !net.ParseIP(host).IsLoopback() {
				http.Error(w, "admin is available only from localhost", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}
func (a *App) admin(w http.ResponseWriter, r *http.Request)       { a.localAdmin(a.renderAdmin)(w, r) }
func (a *App) adminStatus(w http.ResponseWriter, r *http.Request) { a.localAdmin(a.renderStatus)(w, r) }
func (a *App) data(ctx context.Context) (adminData, error) {
	users, err := a.store.LinkedUsers(ctx)
	return adminData{LinkedUsers: users}, err
}

func (a *App) SyncWeight(ctx context.Context, userID string) (int, error) {
	cursor, err := a.store.SyncCursor(ctx, userID, "weight")
	if err != nil {
		return 0, fmt.Errorf("load weight cursor: %w", err)
	}
	token, err := a.AccessToken(ctx, userID)
	if err != nil {
		return 0, err
	}
	result, err := a.withings.FetchMeasurements(ctx, token, cursor)
	if err != nil {
		return 0, err
	}
	normalized := withings.NormalizeMeasurements(result.Groups)
	measurements := make([]store.Measurement, 0, len(normalized))
	for _, measurement := range normalized {
		measurements = append(measurements, store.Measurement{WithingsUserID: userID, SourceGroupID: measurement.SourceGroupID, Metric: measurement.Metric, Value: measurement.Value, Unit: measurement.Unit, ObservedAt: measurement.ObservedAt})
	}
	return a.store.ImportMeasurements(ctx, userID, "weight", measurements, result.UpdateTime)
}

func (a *App) manualWeightSync(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		count, err := a.SyncWeight(r.Context(), r.PathValue("id"))
		result := syncResult{Message: fmt.Sprintf("Imported %d new scale measurements.", count)}
		if err != nil {
			result = syncResult{Message: "Weight sync failed.", Failed: true}
		}
		if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = a.templates.ExecuteTemplate(w, "sync-result", result)
			return
		}
		values := url.Values{"sync": {result.Message}}
		if result.Failed {
			values.Set("sync_failed", "true")
		}
		http.Redirect(w, r, "/admin?"+values.Encode(), http.StatusSeeOther)
	})(w, r)
}

func (a *App) unlinkWithings(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		if err := a.store.DeleteLinkedUser(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, "could not unlink account", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})(w, r)
}
func (a *App) renderAdmin(w http.ResponseWriter, r *http.Request) {
	data, err := a.data(r.Context())
	if err != nil {
		http.Error(w, "database unavailable", 500)
		return
	}
	data.SyncMessage = r.URL.Query().Get("sync")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.templates.ExecuteTemplate(w, "admin", data)
}
func (a *App) renderStatus(w http.ResponseWriter, r *http.Request) {
	data, err := a.data(r.Context())
	if err != nil {
		http.Error(w, "database unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "HX-Request")
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		_ = a.templates.ExecuteTemplate(w, "status", data)
		return
	}
	_ = a.templates.ExecuteTemplate(w, "admin", data)
}
