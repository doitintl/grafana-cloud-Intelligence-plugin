package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// upstreamBodyMarker marks content that must never reach users: it is
// returned in upstream response bodies by the test servers below.
const upstreamBodyMarker = "UPSTREAM-BODY-SHOULD-NOT-LEAK"

func healthRequest() *backend.CheckHealthRequest {
	return &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				JSONData:                []byte(`{}`),
				DecryptedSecureJSONData: map[string]string{"apiKey": "test-key"},
			},
		},
	}
}

func upstreamStatusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"` + upstreamBodyMarker + `"}`))
	})
}

// assertSanitized fails when a user-facing message carries upstream response
// bodies, status codes, or transport details.
func assertSanitized(t *testing.T, message, serverURL string) {
	t.Helper()

	for _, forbidden := range []string{upstreamBodyMarker, "status ", "dial tcp", "connection refused", "127.0.0.1", "localhost"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("message %q leaks %q", message, forbidden)
		}
	}

	if serverURL != "" {
		host := strings.TrimPrefix(serverURL, "http://")
		if strings.Contains(message, host) {
			t.Errorf("message %q leaks upstream host %q", message, host)
		}
	}
}

func TestCheckHealth_OK(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"dimensions":[]}`))
	})

	ds, server := newTestDatasource(handler)
	defer server.Close()

	result, err := ds.CheckHealth(context.Background(), healthRequest())
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want %v (message %q)", result.Status, backend.HealthStatusOk, result.Message)
	}
}

func TestCheckHealth_MissingAPIKey(t *testing.T) {
	ds, server := newTestDatasource(upstreamStatusHandler(http.StatusUnauthorized))
	defer server.Close()

	req := healthRequest()
	req.PluginContext.DataSourceInstanceSettings.DecryptedSecureJSONData = map[string]string{}

	result, err := ds.CheckHealth(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != backend.HealthStatusError || result.Message != "API key is missing" {
		t.Fatalf("got %v %q", result.Status, result.Message)
	}
}

func TestCheckHealth_SanitizesUpstreamErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		wantMessage string
	}{
		{"unauthorized", http.StatusUnauthorized, "Authentication failed. Check the DoiT API key."},
		{"forbidden", http.StatusForbidden, "Access denied. Check the API key permissions."},
		{"throttled", http.StatusTooManyRequests, "The DoiT API is throttling requests. Wait a moment and try again."},
		{"server error", http.StatusInternalServerError, "The DoiT API is temporarily unavailable. Try again later."},
		{"bad gateway", http.StatusBadGateway, "The DoiT API is temporarily unavailable. Try again later."},
		{"not found", http.StatusNotFound, "The DoiT API returned an unexpected response. Check the API URL."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, server := newTestDatasource(upstreamStatusHandler(tt.status))
			defer server.Close()

			result, err := ds.CheckHealth(context.Background(), healthRequest())
			if err != nil {
				t.Fatal(err)
			}

			if result.Status != backend.HealthStatusError {
				t.Fatalf("status = %v, want error", result.Status)
			}

			if result.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", result.Message, tt.wantMessage)
			}

			assertSanitized(t, result.Message, server.URL)
		})
	}
}

func TestCheckHealth_SanitizesConnectionErrors(t *testing.T) {
	ds, server := newTestDatasource(upstreamStatusHandler(http.StatusOK))
	// Closing the server before the check makes every request fail at the
	// transport level, the same way an unreachable API URL would.
	server.Close()

	result, err := ds.CheckHealth(context.Background(), healthRequest())
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != backend.HealthStatusError {
		t.Fatalf("status = %v, want error", result.Status)
	}

	want := "Could not reach the DoiT API. Check the API URL and network connectivity."
	if result.Message != want {
		t.Errorf("message = %q, want %q", result.Message, want)
	}

	assertSanitized(t, result.Message, server.URL)
}

func callResource(t *testing.T, ds *Datasource, target string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ds.resourceMux().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

	return recorder
}

func TestResourceErrors_SanitizeUpstreamErrors(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		status      int
		wantStatus  int
		wantMessage string
	}{
		{"reports unauthorized", "/reports", http.StatusUnauthorized, http.StatusUnauthorized, "Authentication failed. Check the DoiT API key."},
		{"dimensions forbidden", "/dimensions", http.StatusForbidden, http.StatusForbidden, "Access denied. Check the API key permissions."},
		{"dimension values not found", "/dimension-values?type=fixed&id=service", http.StatusNotFound, http.StatusNotFound, "The report or query was not found."},
		{"reports server error", "/reports", http.StatusInternalServerError, http.StatusBadGateway, "The DoiT API is temporarily unavailable. Try again later."},
		{"reports throttled", "/reports", http.StatusTooManyRequests, http.StatusTooManyRequests, "Too many report queries are running. Wait before refreshing."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, server := newTestDatasource(upstreamStatusHandler(tt.status))
			defer server.Close()

			recorder := callResource(t, ds, tt.target)

			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			body := strings.TrimSpace(recorder.Body.String())
			if body != tt.wantMessage {
				t.Errorf("body = %q, want %q", body, tt.wantMessage)
			}

			assertSanitized(t, body, server.URL)
		})
	}
}

func TestResourceErrors_SanitizeConnectionErrors(t *testing.T) {
	ds, server := newTestDatasource(upstreamStatusHandler(http.StatusOK))
	server.Close()

	for _, target := range []string{"/reports", "/dimensions", "/dimension-values?type=fixed&id=service"} {
		recorder := callResource(t, ds, target)

		if recorder.Code != http.StatusBadGateway {
			t.Errorf("%s: status = %d, want %d", target, recorder.Code, http.StatusBadGateway)
		}

		body := strings.TrimSpace(recorder.Body.String())
		if body != "Could not reach the DoiT API. Try again." {
			t.Errorf("%s: body = %q", target, body)
		}

		assertSanitized(t, body, server.URL)
	}
}

func TestResourceErrors_MissingDimensionParams(t *testing.T) {
	ds, server := newTestDatasource(upstreamStatusHandler(http.StatusOK))
	defer server.Close()

	recorder := callResource(t, ds, "/dimension-values?type=fixed")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
