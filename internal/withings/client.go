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
	} `json:"body"`
}

func (c Client) FetchMeasurements(ctx context.Context, accessToken string, lastUpdate int64) (Measurements, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	values := url.Values{"action": {"getmeas"}, "lastupdate": {strconv.FormatInt(lastUpdate, 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/measure", strings.NewReader(values.Encode()))
	if err != nil {
		return Measurements{}, fmt.Errorf("create measurement request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Measurements{}, fmt.Errorf("fetch measurements: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Measurements{}, fmt.Errorf("fetch measurements: unexpected HTTP status %d", resp.StatusCode)
	}
	var decoded measureResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Measurements{}, fmt.Errorf("decode measurements: %w", err)
	}
	if decoded.Status != 0 {
		return Measurements{}, fmt.Errorf("fetch measurements: Withings status %d", decoded.Status)
	}
	return Measurements{UpdateTime: decoded.Body.UpdateTime, Groups: decoded.Body.Groups}, nil
}
