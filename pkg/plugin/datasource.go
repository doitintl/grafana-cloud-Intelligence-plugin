package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/models"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

const (
	queryTypeSavedReport = "report"
	queryTypeAdHoc       = "query"

	dateFormat = "2006-01-02"

	reportQueryTimeout = 5 * time.Minute
)

func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	pluginSettings, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}

	httpOptions, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		return nil, err
	}

	// Cold BigQuery report runs can far exceed the SDK's default 30s HTTP timeout.
	if httpOptions.Timeouts == nil {
		defaults := httpclient.DefaultTimeoutOptions
		httpOptions.Timeouts = &defaults
	}

	if httpOptions.Timeouts.Timeout < reportQueryTimeout {
		httpOptions.Timeouts.Timeout = reportQueryTimeout
	}

	httpClient, err := httpclient.New(httpOptions)
	if err != nil {
		return nil, err
	}

	ds := &Datasource{
		client: doitapi.NewClient(pluginSettings.APIURL, pluginSettings.Secrets.APIKey, httpClient),
	}
	ds.resourceHandler = httpadapter.New(ds.resourceMux())

	return ds, nil
}

type Datasource struct {
	client          *doitapi.Client
	resourceHandler backend.CallResourceHandler
}

func (d *Datasource) Dispose() {}

type queryModel struct {
	QueryType      string          `json:"doitQueryType"`
	ReportID       string          `json:"reportId"`
	ReportName     string          `json:"reportName"`
	Config         json.RawMessage `json:"config"`
	UseGrafanaTime bool            `json:"useGrafanaTimeRange"`
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}

	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err))
	}

	switch qm.QueryType {
	case queryTypeSavedReport, "":
		return d.querySavedReport(ctx, query, qm)
	case queryTypeAdHoc:
		return d.queryAdHoc(ctx, qm)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("unknown query type: %s", qm.QueryType))
	}
}

func (d *Datasource) querySavedReport(ctx context.Context, query backend.DataQuery, qm queryModel) backend.DataResponse {
	if qm.ReportID == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "report is not selected")
	}

	startDate, endDate := "", ""
	if qm.UseGrafanaTime {
		startDate = query.TimeRange.From.UTC().Format(dateFormat)
		endDate = query.TimeRange.To.UTC().Format(dateFormat)
	}

	resp, err := d.client.RunReport(ctx, qm.ReportID, startDate, endDate)
	if err != nil {
		return apiErrorResponse(err)
	}

	name := resp.ReportName
	if name == "" {
		name = qm.ReportID
	}

	frames, err := FramesFromResult(name, &resp.Result)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform result: %v", err))
	}

	return backend.DataResponse{Frames: frames}
}

func (d *Datasource) queryAdHoc(ctx context.Context, qm queryModel) backend.DataResponse {
	if len(qm.Config) == 0 {
		return backend.ErrDataResponse(backend.StatusBadRequest, "query config is empty")
	}

	resp, err := d.client.RunQuery(ctx, qm.Config)
	if err != nil {
		return apiErrorResponse(err)
	}

	frames, err := FramesFromResult("query", &resp.Result)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform result: %v", err))
	}

	return backend.DataResponse{Frames: frames}
}

func apiErrorResponse(err error) backend.DataResponse {
	var apiErr *doitapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return backend.ErrDataResponse(backend.StatusUnauthorized, apiErr.Error())
		case http.StatusNotFound:
			return backend.ErrDataResponse(backend.StatusNotFound, apiErr.Error())
		case http.StatusTooManyRequests:
			return backend.ErrDataResponse(backend.StatusTooManyRequests, apiErr.Error())
		default:
			return backend.ErrDataResponse(backend.StatusBadRequest, apiErr.Error())
		}
	}

	return backend.ErrDataResponse(backend.StatusInternal, err.Error())
}

func (d *Datasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	config, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Unable to load settings",
		}, nil
	}

	if config.Secrets.APIKey == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "API key is missing",
		}, nil
	}

	if _, err := d.client.ListDimensions(ctx); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("Cannot reach DoiT API: %v", err),
		}, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Successfully connected to the DoiT API",
	}, nil
}

func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return d.resourceHandler.CallResource(ctx, req, sender)
}

func (d *Datasource) resourceMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /reports", func(w http.ResponseWriter, r *http.Request) {
		reports, err := d.client.ListReports(r.Context())
		if err != nil {
			writeResourceError(w, err)
			return
		}

		writeJSON(w, reports)
	})

	mux.HandleFunc("GET /dimensions", func(w http.ResponseWriter, r *http.Request) {
		dimensions, err := d.client.ListDimensions(r.Context())
		if err != nil {
			writeResourceError(w, err)
			return
		}

		writeJSON(w, dimensions)
	})

	mux.HandleFunc("GET /dimension-values", func(w http.ResponseWriter, r *http.Request) {
		dimensionType := r.URL.Query().Get("type")
		dimensionID := r.URL.Query().Get("id")

		if dimensionType == "" || dimensionID == "" {
			http.Error(w, "type and id query params are required", http.StatusBadRequest)
			return
		}

		dimension, err := d.client.GetDimensionValues(r.Context(), dimensionType, dimensionID)
		if err != nil {
			writeResourceError(w, err)
			return
		}

		writeJSON(w, dimension)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeResourceError(w http.ResponseWriter, err error) {
	var apiErr *doitapi.APIError
	if errors.As(err, &apiErr) {
		http.Error(w, apiErr.Error(), apiErr.StatusCode)
		return
	}

	http.Error(w, err.Error(), http.StatusInternalServerError)
}
