package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

func newTestDatasource(handler http.Handler) (*Datasource, *httptest.Server) {
	server := httptest.NewServer(handler)

	ds := &Datasource{
		client: doitapi.NewClient(server.URL, "test-key", server.Client()),
	}
	ds.resourceHandler = nil

	return ds, server
}

func TestQueryData_SavedReport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/analytics/v1/reports/report-1", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/analytics/v1/reports/query", func(w http.ResponseWriter, r *http.Request) {
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

	queryJSON, _ := json.Marshal(map[string]any{
		"doitQueryType": "query",
		"config":        map[string]any{"metric": map[string]string{"type": "basic", "value": "cost"}},
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
