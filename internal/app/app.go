package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Drakmyth/withings2mqtt/internal/config"
	crypt "github.com/Drakmyth/withings2mqtt/internal/crypto"
	"github.com/Drakmyth/withings2mqtt/internal/store"
	"github.com/Drakmyth/withings2mqtt/internal/withings"
)

//go:embed templates/*.html static/*
var assets embed.FS

const withingsAuthorizeURL = "https://account.withings.com/oauth2_user/authorize2"
const withingsTokenURL = "https://wbsapi.withings.net/v2/oauth2"

type oauthState struct{ expiresAt time.Time }

type App struct {
	cfg         config.Config
	store       *store.Store
	cipher      *crypt.TokenCipher
	templates   *template.Template
	httpClient  *http.Client
	tokenURL    string
	withings    withings.Client
	statesMu    sync.Mutex
	states      map[string]oauthState
	syncLocksMu sync.Mutex
	syncLocks   map[string]*sync.Mutex
}
type adminData struct {
	LinkedUsers []store.LinkedUser
	SyncMessage string
}

type syncResult struct {
	Message string
	Failed  bool
}

type scalePageData struct {
	UserID        string
	Latest        []store.Measurement
	SyncStatus    store.SyncCursorStatus
	History       store.MeasurementPage
	Metric        string
	MetricOptions []string
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
	templates, err := template.New("").Funcs(template.FuncMap{
		"formatMetric": formatMetric,
		"formatTime":   func(value time.Time) string { return value.UTC().Format("2006-01-02 15:04 UTC") },
		"formatValue":  func(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) },
		"pageURL":      scalePageURL,
		"sub":          func(value int) int { return value - 1 },
		"add":          func(value int) int { return value + 1 },
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		s.Close()
		return nil, err
	}
	return &App{cfg: cfg, store: s, cipher: cipher, templates: templates, httpClient: http.DefaultClient, tokenURL: withingsTokenURL, withings: withings.Client{BaseURL: withings.DefaultBaseURL, HTTPClient: http.DefaultClient}, states: make(map[string]oauthState), syncLocks: make(map[string]*sync.Mutex)}, nil
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
	mux.HandleFunc("POST /admin/withings/{id}/sync/weight/all", noStore(a.manualFullWeightSync))
	mux.HandleFunc("POST /admin/withings/{id}/sync/activity", noStore(a.manualActivitySync))
	mux.HandleFunc("GET /admin/withings/{id}/scale", noStore(a.scalePage))
	mux.HandleFunc("GET /admin/withings/{id}/scale/history", noStore(a.scaleHistory))
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return mux
}

func formatMetric(metric string) string {
	labels := map[string]string{"weight": "Weight", "fat_free_mass": "Fat-free mass", "fat_ratio": "Fat ratio", "fat_mass": "Fat mass", "muscle_mass": "Muscle mass", "hydration": "Hydration", "bone_mass": "Bone mass"}
	if label := labels[metric]; label != "" {
		return label
	}
	return metric
}

func scalePageURL(userID, metric string, page int) string {
	values := url.Values{"page": {strconv.Itoa(page)}}
	if metric != "" {
		values.Set("metric", metric)
	}
	return "/admin/withings/" + url.PathEscape(userID) + "/scale/history?" + values.Encode()
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
	return a.syncWeightFrom(ctx, userID, cursor)
}

func (a *App) SyncAllScaleData(ctx context.Context, userID string) (int, error) {
	return a.syncWeightFrom(ctx, userID, 0)
}

func (a *App) syncWeightFrom(ctx context.Context, userID string, cursor int64) (int, error) {
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

func (a *App) SyncActivity(ctx context.Context, userID string) (int, error) {
	unlock := a.lockSync(userID, "activity")
	defer unlock()

	cursor, err := a.store.SyncCursor(ctx, userID, "activity")
	if err != nil {
		return 0, fmt.Errorf("load activity cursor: %w", err)
	}
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -30)
	if cursor > 0 {
		startDate = time.Unix(cursor, 0).UTC().Truncate(24 * time.Hour)
	}
	token, err := a.AccessToken(ctx, userID)
	if err != nil {
		return 0, err
	}
	result, err := a.withings.FetchDailyActivity(ctx, token, startDate, endDate)
	if err != nil {
		return 0, err
	}
	activities := make([]store.DailyActivity, 0, len(result))
	for _, activity := range result {
		observedAt, err := activityObservedAt(activity.Date, activity.Timezone)
		if err != nil {
			return 0, err
		}
		activities = append(activities, store.DailyActivity{
			Date: activity.Date, Steps: activity.Steps, StepsUnit: "steps",
			Distance: activity.Distance, DistanceUnit: "m",
			Calories: activity.Calories, CaloriesUnit: "kcal",
			Elevation: activity.Elevation, ElevationUnit: "m",
			ObservedAt: observedAt,
		})
	}
	return a.store.ImportDailyActivities(ctx, userID, activities, endDate.Unix())
}

func (a *App) lockSync(userID, category string) func() {
	key := userID + "\x00" + category
	a.syncLocksMu.Lock()
	lock := a.syncLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		a.syncLocks[key] = lock
	}
	a.syncLocksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func activityObservedAt(date, timezone string) (time.Time, error) {
	location := time.UTC
	if timezone != "" {
		var err error
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("load activity timezone %q: %w", timezone, err)
		}
	}
	observedAt, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse activity date %q: %w", date, err)
	}
	return observedAt, nil
}

func (a *App) manualActivitySync(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		count, err := a.SyncActivity(r.Context(), r.PathValue("id"))
		result := syncResult{Message: fmt.Sprintf("Imported or updated %d daily activity summaries.", count)}
		if err != nil {
			log.Printf("activity sync failed for Withings user %q: %v", r.PathValue("id"), err)
			result = syncResult{Message: "Activity sync failed.", Failed: true}
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

func (a *App) manualWeightSync(w http.ResponseWriter, r *http.Request) {
	a.manualScaleSync(w, r, false)
}

func (a *App) manualFullWeightSync(w http.ResponseWriter, r *http.Request) {
	a.manualScaleSync(w, r, true)
}

func (a *App) manualScaleSync(w http.ResponseWriter, r *http.Request, full bool) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		var count int
		var err error
		if full {
			count, err = a.SyncAllScaleData(r.Context(), r.PathValue("id"))
		} else {
			count, err = a.SyncWeight(r.Context(), r.PathValue("id"))
		}
		result := syncResult{Message: fmt.Sprintf("Imported or updated %d scale measurements.", count)}
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

var scaleMetrics = []string{"weight", "fat_free_mass", "fat_ratio", "fat_mass", "muscle_mass", "hydration", "bone_mass"}

func (a *App) scalePage(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) { a.renderScale(w, r, false) })(w, r)
}

func (a *App) scaleHistory(w http.ResponseWriter, r *http.Request) {
	a.localAdmin(func(w http.ResponseWriter, r *http.Request) {
		a.renderScale(w, r, strings.EqualFold(r.Header.Get("HX-Request"), "true"))
	})(w, r)
}

func (a *App) renderScale(w http.ResponseWriter, r *http.Request, fragment bool) {
	userID := r.PathValue("id")
	if _, err := a.store.LinkedUser(r.Context(), userID); err != nil {
		http.Error(w, "linked account not found", http.StatusNotFound)
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric != "" {
		valid := false
		for _, option := range scaleMetrics {
			valid = valid || metric == option
		}
		if !valid {
			http.Error(w, "invalid scale metric", http.StatusBadRequest)
			return
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	latest, err := a.store.LatestMeasurements(r.Context(), userID)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusInternalServerError)
		return
	}
	history, err := a.store.MeasurementHistory(r.Context(), userID, metric, page, 50)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusInternalServerError)
		return
	}
	status, err := a.store.SyncStatus(r.Context(), userID, "weight")
	if err != nil {
		http.Error(w, "database unavailable", http.StatusInternalServerError)
		return
	}
	data := scalePageData{UserID: userID, Latest: latest, SyncStatus: status, History: history, Metric: metric, MetricOptions: scaleMetrics}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "HX-Request")
	if fragment {
		_ = a.templates.ExecuteTemplate(w, "scale-history", data)
		return
	}
	_ = a.templates.ExecuteTemplate(w, "scale", data)
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
