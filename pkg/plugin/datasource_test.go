package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

func newTestDatasource(handler http.Handler) (*Datasource, *httptest.Server) {
	server := httptest.NewServer(handler)

	client := doitapi.NewClient(server.URL, "test-key", server.Client())
	ds := newDatasource(client, client, time.Now)
	ds.resourceHandler = nil

	return ds, server
}

type queryAPIStub struct {
	runReport func(context.Context, string, string, string) (*doitapi.RunReportResponse, error)
}

func (s queryAPIStub) RunReport(
	ctx context.Context,
	reportID, startDate, endDate string,
) (*doitapi.RunReportResponse, error) {
	return s.runReport(ctx, reportID, startDate, endDate)
}

func (queryAPIStub) RunQuery(context.Context, json.RawMessage) (*doitapi.RunQueryResponse, error) {
	return nil, errors.New("unexpected ad-hoc query")
}

func TestQueryData_SavedReport(t *testing.T) {
	mux := http.NewServeMux()
	var calls atomic.Int32
	mux.HandleFunc("/analytics/v1/reports/report-1", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "report-1",
			"reportName": "Test Report",
			"result": map[string]any{
				"schema": []map[string]string{
					{"name": "cost", "type": "float"},
				},
				"rows": [][]any{{12.34}},
			},
		})
	})

	ds, server := newTestDatasource(mux)
	defer server.Close()

	queryJSON, _ := json.Marshal(map[string]any{
		"doitQueryType": "report",
		"reportId":      "report-1",
	})

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}

	res := resp.Responses["A"]
	if res.Error != nil {
		t.Fatalf("unexpected query error: %v", res.Error)
	}

	if len(res.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(res.Frames))
	}

	if res.Frames[0].Name != "Test Report" {
		t.Errorf("unexpected frame name: %s", res.Frames[0].Name)
	}

	if _, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	}); err != nil {
		t.Fatal(err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("saved report API calls = %d, want 1", got)
	}
}

func TestQueryData_SavedReportTreemapFormat(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/analytics/v1/reports/report-1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "report-1",
			"reportName": "Services Breakdown",
			"result": map[string]any{
				"schema": []map[string]string{
					{"name": "service_description", "type": "string"},
					{"name": "sku_description", "type": "string"},
					{"name": "cost", "type": "float"},
					{"name": "timestamp", "type": "timestamp"},
				},
				"rows": [][]any{{"BigQuery", "Analysis", 12.34, 0}},
			},
		})
	})

	ds, server := newTestDatasource(mux)
	defer server.Close()

	queryJSON, _ := json.Marshal(map[string]any{
		"doitQueryType": "report",
		"reportId":      "report-1",
		"resultFormat":  "treemap",
	})

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := resp.Responses["A"].Frames[0]
	if got := frame.Fields[0].At(0); got != "BigQuery"+treemapHierarchySeparator+"Analysis" {
		t.Errorf("unexpected hierarchy path: %v", got)
	}
	if got := frame.Fields[1].At(0); got != 12.34 {
		t.Errorf("unexpected treemap value: %v", got)
	}
}

func TestQueryData_MissingReport(t *testing.T) {
	ds, server := newTestDatasource(http.NewServeMux())
	defer server.Close()

	queryJSON, _ := json.Marshal(map[string]any{"doitQueryType": "report"})

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Responses["A"].Error == nil {
		t.Fatal("expected error for missing report id")
	}
}

func TestQueryData_AdHoc(t *testing.T) {
	mux := http.NewServeMux()
	var calls atomic.Int32
	mux.HandleFunc("/analytics/v1/reports/query", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("bad request body: %v", err)
		}

		if _, ok := body["config"]; !ok {
			t.Error("expected config in request body")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"schema": []map[string]string{
					{"name": "service_description", "type": "string"},
					{"name": "cost", "type": "float"},
				},
				"rows": [][]any{{"BigQuery", 10.5}},
			},
		})
	})

	ds, server := newTestDatasource(mux)
	defer server.Close()

	queryJSON := json.RawMessage(`{
		"doitQueryType": "query",
		"config": {
			"metric": {"type": "basic", "value": "cost"},
			"aggregation": "total"
		}
	}`)
	equivalentQueryJSON := json.RawMessage(`{
		"config": {"aggregation":"total","metric":{"value":"cost","type":"basic"}},
		"doitQueryType": "query"
	}`)

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}

	res := resp.Responses["A"]
	if res.Error != nil {
		t.Fatalf("unexpected query error: %v", res.Error)
	}

	if len(res.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(res.Frames))
	}

	if _, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: equivalentQueryJSON}},
	}); err != nil {
		t.Fatal(err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("ad-hoc query API calls = %d, want 1", got)
	}
}

func TestQueryData_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/analytics/v1/reports/report-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ds, server := newTestDatasource(mux)
	defer server.Close()

	queryJSON, _ := json.Marshal(map[string]any{
		"doitQueryType": "report",
		"reportId":      "report-1",
	})

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Responses["A"].Error == nil {
		t.Fatal("expected error from forbidden API response")
	}
}

func TestAPIErrorResponse(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantStatus   backend.Status
		wantSource   backend.ErrorSource
		wantMessage  string
		wantIdentity bool
	}{
		{
			name:        "bad request",
			err:         &doitapi.APIError{StatusCode: http.StatusBadRequest},
			wantStatus:  backend.StatusBadRequest,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "Invalid query. Check the report or query configuration.",
		},
		{
			name:        "unauthenticated",
			err:         &doitapi.APIError{StatusCode: http.StatusUnauthorized},
			wantStatus:  backend.StatusUnauthorized,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "Authentication failed. Check the DoiT API key.",
		},
		{
			name:        "forbidden",
			err:         &doitapi.APIError{StatusCode: http.StatusForbidden},
			wantStatus:  backend.StatusForbidden,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "Access denied. Check the API key permissions.",
		},
		{
			name:        "not found",
			err:         &doitapi.APIError{StatusCode: http.StatusNotFound},
			wantStatus:  backend.StatusNotFound,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The report or query was not found.",
		},
		{
			name:        "request timeout",
			err:         &doitapi.APIError{StatusCode: http.StatusRequestTimeout},
			wantStatus:  backend.StatusTimeout,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The report query timed out. Try a shorter time range.",
		},
		{
			name:        "gateway timeout",
			err:         &doitapi.APIError{StatusCode: http.StatusGatewayTimeout},
			wantStatus:  backend.StatusTimeout,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The report query timed out. Try a shorter time range.",
		},
		{
			name:        "cloudflare timeout",
			err:         &doitapi.APIError{StatusCode: 524},
			wantStatus:  backend.StatusTimeout,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The report query timed out. Try a shorter time range.",
		},
		{
			name:        "throttled",
			err:         &doitapi.APIError{StatusCode: http.StatusTooManyRequests},
			wantStatus:  backend.StatusTooManyRequests,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "Too many report queries are running. Wait before refreshing.",
		},
		{
			name:        "upstream failure",
			err:         &doitapi.APIError{StatusCode: http.StatusServiceUnavailable},
			wantStatus:  backend.StatusBadGateway,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The DoiT API is temporarily unavailable. Try again later.",
		},
		{
			name:        "other client error",
			err:         &doitapi.APIError{StatusCode: http.StatusUnprocessableEntity},
			wantStatus:  backend.StatusBadRequest,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The DoiT API rejected the query. Check its configuration.",
		},
		{
			name:        "transport deadline",
			err:         context.DeadlineExceeded,
			wantStatus:  backend.StatusTimeout,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "The report query timed out. Try a shorter time range.",
		},
		{
			name:         "transport cancellation",
			err:          context.Canceled,
			wantStatus:   backend.StatusInternal,
			wantSource:   backend.ErrorSourcePlugin,
			wantMessage:  context.Canceled.Error(),
			wantIdentity: true,
		},
		{
			name:        "local query queue full",
			err:         errQueryQueueFull,
			wantStatus:  backend.StatusTooManyRequests,
			wantSource:  backend.ErrorSourcePlugin,
			wantMessage: "Too many report queries are running. Wait before refreshing.",
		},
		{
			name:        "coordinator closed",
			err:         errCoordinatorClosed,
			wantStatus:  backend.StatusInternal,
			wantSource:  backend.ErrorSourcePlugin,
			wantMessage: "The data source is shutting down.",
		},
		{
			name:        "network failure",
			err:         &net.DNSError{Err: "unreachable", Name: "api.doit.com"},
			wantStatus:  backend.StatusBadGateway,
			wantSource:  backend.ErrorSourceDownstream,
			wantMessage: "Could not reach the DoiT API. Try again.",
		},
		{
			name:        "local transform failure",
			err:         errors.New("transform result"),
			wantStatus:  backend.StatusInternal,
			wantSource:  backend.ErrorSourcePlugin,
			wantMessage: "The query failed. Try again.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := apiErrorResponse(tt.err)

			if response.Status != tt.wantStatus {
				t.Errorf("status = %v, want %v", response.Status, tt.wantStatus)
			}
			if !response.Status.IsValid() {
				t.Errorf("status = %v is not valid", response.Status)
			}
			if response.ErrorSource != tt.wantSource {
				t.Errorf("source = %v, want %v", response.ErrorSource, tt.wantSource)
			}
			if response.Error == nil || response.Error.Error() != tt.wantMessage {
				t.Errorf("message = %v, want %q", response.Error, tt.wantMessage)
			}
			if tt.wantIdentity && !errors.Is(response.Error, tt.err) {
				t.Errorf("error = %v, want identity %v", response.Error, tt.err)
			}
		})
	}
}

func TestQueryDataReturnsCanceledContextBeforeProcessing(t *testing.T) {
	var calls atomic.Int32
	ds := newDatasource(queryAPIStub{
		runReport: func(context.Context, string, string, string) (*doitapi.RunReportResponse, error) {
			calls.Add(1)
			return &doitapi.RunReportResponse{}, nil
		},
	}, nil, time.Now)
	defer ds.Dispose()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	queryJSON := json.RawMessage(`{"doitQueryType":"report","reportId":"report-1"}`)
	response, err := ds.QueryData(ctx, &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("QueryData error = %v, want context canceled", err)
	}
	if response == nil {
		t.Fatal("QueryData returned nil response")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("query calls = %d, want 0", got)
	}
}

func TestQueryDataReturnsCanceledContextAfterProcessing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var calls atomic.Int32
	ds := newDatasource(queryAPIStub{
		runReport: func(context.Context, string, string, string) (*doitapi.RunReportResponse, error) {
			calls.Add(1)
			cancel()

			return &doitapi.RunReportResponse{ReportName: "Report"}, nil
		},
	}, nil, time.Now)
	defer ds.Dispose()

	queryJSON := json.RawMessage(`{"doitQueryType":"report","reportId":"report-1"}`)
	response, err := ds.QueryData(ctx, &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: queryJSON}},
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("QueryData error = %v, want context canceled", err)
	}
	if response == nil {
		t.Fatal("QueryData returned nil response")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("query calls = %d, want 1", got)
	}
}

func TestDatasourceDisposeClosesCoordinator(t *testing.T) {
	coordinator := newQueryCoordinator(time.Now)
	ds := &Datasource{queries: coordinator}

	ds.Dispose()
	ds.Dispose()

	key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
	_, err := coordinator.do(t.Context(), key, func(context.Context) (cachedQueryResult, error) {
		return cachedQueryResult{name: "unexpected"}, nil
	})
	if !errors.Is(err, errCoordinatorClosed) {
		t.Fatalf("query after dispose error = %v, want coordinator closed", err)
	}
}
