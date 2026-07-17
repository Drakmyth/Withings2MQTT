package withings

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchMeasurementsDecodesWeight(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("Authorization = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("action"); got != "getmeas" {
			t.Errorf("action = %q", got)
		}
		if got := r.Form.Get("lastupdate"); got != "1700000000" {
			t.Errorf("lastupdate = %q", got)
		}
		_, _ = w.Write([]byte(`{"status":0,"body":{"updatetime":1700000100,"measuregrps":[{"grpid":42,"date":1700000050,"modified":1700000060,"category":1,"measures":[{"value":80425,"type":1,"unit":-3}]}]}}`))
	}))
	defer server.Close()

	result, err := (Client{BaseURL: server.URL}).FetchMeasurements(context.Background(), "access-token", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdateTime != 1700000100 || len(result.Groups) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	group := result.Groups[0]
	if group.ID != 42 || !group.ObservedAt().Equal(time.Unix(1700000050, 0).UTC()) {
		t.Fatalf("unexpected group: %+v", group)
	}
	if got := group.Measures[0].RealValue(); math.Abs(got-80.425) > 0.000001 {
		t.Fatalf("weight = %v", got)
	}
}

func TestFetchMeasurementsRejectsWithingsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":401,"body":{}}`))
	}))
	defer server.Close()
	if _, err := (Client{BaseURL: server.URL}).FetchMeasurements(context.Background(), "token", 0); err == nil {
		t.Fatal("expected Withings error")
	}
}

func TestFetchMeasurementsFollowsPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if requests == 1 {
			if r.Form.Get("offset") != "" {
				t.Errorf("first offset = %q", r.Form.Get("offset"))
			}
			_, _ = w.Write([]byte(`{"status":0,"body":{"updatetime":300,"more":1,"offset":1000,"measuregrps":[{"grpid":1,"date":100,"category":1,"measures":[{"value":80,"type":1,"unit":0}]}]}}`))
			return
		}
		if r.Form.Get("offset") != "1000" {
			t.Errorf("second offset = %q", r.Form.Get("offset"))
		}
		_, _ = w.Write([]byte(`{"status":0,"body":{"updatetime":300,"more":0,"offset":1000,"measuregrps":[{"grpid":2,"date":200,"category":1,"measures":[{"value":81,"type":1,"unit":0}]}]}}`))
	}))
	defer server.Close()
	result, err := (Client{BaseURL: server.URL}).FetchMeasurements(context.Background(), "token", 0)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(result.Groups) != 2 || result.Groups[1].ID != 2 {
		t.Fatalf("result = %+v after %d requests", result, requests)
	}
}

func TestNormalizeMeasurementsHandlesOptionalComposition(t *testing.T) {
	groups := []MeasureGroup{{
		ID: 42, Date: 1700000000, Category: 1,
		Measures: []Measure{
			{Value: 80425, Type: MeasureWeight, Unit: -3},
			{Value: 205, Type: MeasureFatRatio, Unit: -1},
			{Value: 999, Type: 999, Unit: 0},
		},
	}}
	got := NormalizeMeasurements(groups)
	if len(got) != 2 {
		t.Fatalf("normalized measurements = %+v", got)
	}
	if got[0].Metric != "weight" || math.Abs(got[0].Value-80.425) > 0.000001 || got[0].Unit != "kg" {
		t.Fatalf("weight = %+v", got[0])
	}
	if got[1].Metric != "fat_ratio" || got[1].Value != 20.5 || got[1].Unit != "%" {
		t.Fatalf("fat ratio = %+v", got[1])
	}

	weightOnly := NormalizeMeasurements([]MeasureGroup{{ID: 43, Date: 1700000001, Category: 1, Measures: []Measure{{Value: 81, Type: MeasureWeight, Unit: 0}}}})
	if len(weightOnly) != 1 || weightOnly[0].Metric != "weight" {
		t.Fatalf("missing optional composition was not harmless: %+v", weightOnly)
	}
	if got := NormalizeMeasurements([]MeasureGroup{{ID: 44, Date: 1700000002, Category: 2, Measures: []Measure{{Value: 75, Type: MeasureWeight}}}}); len(got) != 0 {
		t.Fatalf("objective group was imported: %+v", got)
	}
}
