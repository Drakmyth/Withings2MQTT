package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
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
	statesMu   sync.Mutex
	states     map[string]oauthState
}
type adminData struct{ LinkedUserCount int }

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
	return &App{cfg: cfg, store: s, cipher: cipher, templates: templates, httpClient: http.DefaultClient, tokenURL: withingsTokenURL, states: make(map[string]oauthState)}, nil
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
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /admin", a.admin)
	mux.HandleFunc("GET /admin/status", a.adminStatus)
	mux.HandleFunc("GET /admin/withings/link", a.startWithingsLink)
	mux.HandleFunc("GET /oauth/withings/callback", a.withingsCallback)
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return mux
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
		UserID       string `json:"userid"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	} `json:"body"`
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
	if err := a.SaveLinkedUser(r.Context(), token.Body.UserID, token.Body.AccessToken, token.Body.RefreshToken, time.Now().Add(time.Duration(token.Body.ExpiresIn)*time.Second)); err != nil {
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
	count, err := a.store.LinkedUserCount(ctx)
	return adminData{LinkedUserCount: count}, err
}
func (a *App) renderAdmin(w http.ResponseWriter, r *http.Request) {
	data, err := a.data(r.Context())
	if err != nil {
		http.Error(w, "database unavailable", 500)
		return
	}
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
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		_ = a.templates.ExecuteTemplate(w, "status", data)
		return
	}
	_ = a.templates.ExecuteTemplate(w, "admin", data)
}
