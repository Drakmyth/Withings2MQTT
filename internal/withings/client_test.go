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
