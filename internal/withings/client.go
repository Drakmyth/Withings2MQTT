package withings

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://wbsapi.withings.net"

const (
	MeasureWeight     = 1
	MeasureFatFree    = 5
	MeasureFatRatio   = 6
	MeasureFatMass    = 8
	MeasureMuscleMass = 76
	MeasureHydration  = 77
	MeasureBoneMass   = 88
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// DailyActivity is one calendar day's activity summary returned by Withings.
// Optional metrics use pointers so an omitted value is distinct from zero.
type DailyActivity struct {
	Date      string   `json:"date"`
	Timezone  string   `json:"timezone"`
	Steps     int      `json:"steps"`
	Distance  *float64 `json:"distance"`
	Calories  *float64 `json:"calories"`
	Elevation *float64 `json:"elevation"`
}

type activityResponse struct {
	Status int `json:"status"`
	Body   struct {
		Activities []DailyActivity `json:"activities"`
		More       int             `json:"more"`
		Offset     int             `json:"offset"`
	} `json:"body"`
}

// FetchDailyActivity returns daily summaries in the inclusive date range.
func (c Client) FetchDailyActivity(ctx context.Context, accessToken string, startDate, endDate time.Time) ([]DailyActivity, error) {
	if startDate.IsZero() || endDate.IsZero() || endDate.Before(startDate) {
		return nil, fmt.Errorf("fetch daily activity: invalid date range")
	}
	var activities []DailyActivity
	offset := 0
	for {
		page, err := c.fetchActivityPage(ctx, accessToken, startDate, endDate, offset)
		if err != nil {
			return nil, err
		}
		activities = append(activities, page.Body.Activities...)
		if page.Body.More == 0 {
			return activities, nil
		}
		if page.Body.Offset <= offset {
			return nil, fmt.Errorf("fetch daily activity: pagination offset did not advance")
		}
		offset = page.Body.Offset
	}
}

func (c Client) fetchActivityPage(ctx context.Context, accessToken string, startDate, endDate time.Time, offset int) (activityResponse, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	values := url.Values{
		"action":       {"getactivity"},
		"startdateymd": {startDate.Format("2006-01-02")},
		"enddateymd":   {endDate.Format("2006-01-02")},
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/measure", strings.NewReader(values.Encode()))
	if err != nil {
		return activityResponse{}, fmt.Errorf("create daily activity request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return activityResponse{}, fmt.Errorf("fetch daily activity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return activityResponse{}, fmt.Errorf("fetch daily activity: unexpected HTTP status %d", resp.StatusCode)
	}
	var decoded activityResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return activityResponse{}, fmt.Errorf("decode daily activity: %w", err)
	}
	if decoded.Status != 0 {
		return activityResponse{}, fmt.Errorf("fetch daily activity: Withings status %d", decoded.Status)
	}
	return decoded, nil
}

type Measure struct {
	Value int64 `json:"value"`
	Type  int   `json:"type"`
	Unit  int   `json:"unit"`
}

func (m Measure) RealValue() float64 { return float64(m.Value) * math.Pow10(m.Unit) }

type MeasureGroup struct {
	ID       int64     `json:"grpid"`
	Date     int64     `json:"date"`
	Modified int64     `json:"modified"`
	Category int       `json:"category"`
	Measures []Measure `json:"measures"`
}

func (g MeasureGroup) ObservedAt() time.Time { return time.Unix(g.Date, 0).UTC() }

type Measurements struct {
	UpdateTime int64
	Groups     []MeasureGroup
}

type NormalizedMeasurement struct {
	SourceGroupID int64
	Metric        string
	Value         float64
	Unit          string
	ObservedAt    time.Time
}

func NormalizeMeasurements(groups []MeasureGroup) []NormalizedMeasurement {
	var normalized []NormalizedMeasurement
	for _, group := range groups {
		if group.Category != 1 {
			continue
		}
		for _, measure := range group.Measures {
			metric, unit, ok := normalizedMetric(measure.Type)
			if !ok {
				continue
			}
			normalized = append(normalized, NormalizedMeasurement{
				SourceGroupID: group.ID,
				Metric:        metric,
				Value:         measure.RealValue(),
				Unit:          unit,
				ObservedAt:    group.ObservedAt(),
			})
		}
	}
	return normalized
}

func normalizedMetric(measureType int) (string, string, bool) {
	switch measureType {
	case MeasureWeight:
		return "weight", "kg", true
	case MeasureFatFree:
		return "fat_free_mass", "kg", true
	case MeasureFatRatio:
		return "fat_ratio", "%", true
	case MeasureFatMass:
		return "fat_mass", "kg", true
	case MeasureMuscleMass:
		return "muscle_mass", "kg", true
	case MeasureHydration:
		return "hydration", "kg", true
	case MeasureBoneMass:
		return "bone_mass", "kg", true
	default:
		return "", "", false
	}
}

type measureResponse struct {
	Status int `json:"status"`
	Body   struct {
		UpdateTime int64          `json:"updatetime"`
		Groups     []MeasureGroup `json:"measuregrps"`
		More       int            `json:"more"`
		Offset     int            `json:"offset"`
	} `json:"body"`
}

func (c Client) FetchMeasurements(ctx context.Context, accessToken string, lastUpdate int64) (Measurements, error) {
	result := Measurements{}
	offset := 0
	for {
		page, err := c.fetchMeasurementPage(ctx, accessToken, lastUpdate, offset)
		if err != nil {
			return Measurements{}, err
		}
		result.Groups = append(result.Groups, page.Body.Groups...)
		if page.Body.UpdateTime > result.UpdateTime {
			result.UpdateTime = page.Body.UpdateTime
		}
		if page.Body.More == 0 {
			return result, nil
		}
		if page.Body.Offset <= offset {
			return Measurements{}, fmt.Errorf("fetch measurements: pagination offset did not advance")
		}
		offset = page.Body.Offset
	}
}

func (c Client) fetchMeasurementPage(ctx context.Context, accessToken string, lastUpdate int64, offset int) (measureResponse, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	values := url.Values{"action": {"getmeas"}, "lastupdate": {strconv.FormatInt(lastUpdate, 10)}}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/measure", strings.NewReader(values.Encode()))
	if err != nil {
		return measureResponse{}, fmt.Errorf("create measurement request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return measureResponse{}, fmt.Errorf("fetch measurements: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return measureResponse{}, fmt.Errorf("fetch measurements: unexpected HTTP status %d", resp.StatusCode)
	}
	var decoded measureResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return measureResponse{}, fmt.Errorf("decode measurements: %w", err)
	}
	if decoded.Status != 0 {
		return measureResponse{}, fmt.Errorf("fetch measurements: Withings status %d", decoded.Status)
	}
	return decoded, nil
}
