package app

import (
	"context"
	"embed"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Drakmyth/withings2mqtt/internal/config"
	crypt "github.com/Drakmyth/withings2mqtt/internal/crypto"
	"github.com/Drakmyth/withings2mqtt/internal/store"
)

//go:embed templates/admin.html static/htmx-2.0.4.min.js
var assets embed.FS

type App struct {
	cfg       config.Config
	store     *store.Store
	cipher    *crypt.TokenCipher
	templates *template.Template
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
	return &App{cfg: cfg, store: s, cipher: cipher, templates: templates}, nil
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
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return mux
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
