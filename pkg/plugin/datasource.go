package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
	"github.com/grafana/grafana-plugin-sdk-go/data"

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
	resultFormatTreemap  = "treemap"

	dateFormat = "2006-01-02"

	reportQueryTimeout = 5 * time.Minute
)

type queryAPI interface {
	RunReport(ctx context.Context, reportID, startDate, endDate string) (*doitapi.RunReportResponse, error)
	RunQuery(ctx context.Context, config json.RawMessage) (*doitapi.RunQueryResponse, error)
}

type metadataAPI interface {
	ListReports(ctx context.Context) ([]doitapi.ReportListItem, error)
	ListDimensions(ctx context.Context) ([]doitapi.Dimension, error)
	GetDimensionValues(ctx context.Context, dimensionType, dimensionID string) (*doitapi.DimensionResponse, error)
}

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

	client := doitapi.NewClient(pluginSettings.APIURL, pluginSettings.Secrets.APIKey, httpClient)
	ds := newDatasource(client, client, time.Now)
	ds.resourceHandler = httpadapter.New(ds.resourceMux())

	return ds, nil
}

type Datasource struct {
	queryClient     queryAPI
	metadataClient  metadataAPI
	queries         *queryCoordinator
	resourceHandler backend.CallResourceHandler
}

func newDatasource(queryClient queryAPI, metadataClient metadataAPI, now func() time.Time) *Datasource {
	return &Datasource{
		queryClient:    queryClient,
		metadataClient: metadataClient,
		queries:        newQueryCoordinator(now),
	}
}

func (d *Datasource) Dispose() {
	if d.queries != nil {
		d.queries.Close()
	}
}

type queryModel struct {
	QueryType      string          `json:"doitQueryType"`
	ReportID       string          `json:"reportId"`
	ReportName     string          `json:"reportName"`
	Config         json.RawMessage `json:"config"`
	UseGrafanaTime bool            `json:"useGrafanaTimeRange"`
	ResultFormat   string          `json:"resultFormat"`
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		if err := ctx.Err(); err != nil {
			return response, err
		}

		response.Responses[q.RefID] = d.query(ctx, q)

		if err := ctx.Err(); err != nil {
			return response, err
		}
	}

	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return pluginErrorResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err))
	}

	switch qm.QueryType {
	case queryTypeSavedReport, "":
		return d.querySavedReport(ctx, query, qm)
	case queryTypeAdHoc:
		return d.queryAdHoc(ctx, qm)
	default:
		return pluginErrorResponse(backend.StatusBadRequest, fmt.Sprintf("unknown query type: %s", qm.QueryType))
	}
}

func (d *Datasource) querySavedReport(ctx context.Context, query backend.DataQuery, qm queryModel) backend.DataResponse {
	if qm.ReportID == "" {
		return pluginErrorResponse(backend.StatusBadRequest, "report is not selected")
	}

	startDate, endDate := "", ""
	if qm.UseGrafanaTime {
		startDate = query.TimeRange.From.UTC().Format(dateFormat)
		endDate = query.TimeRange.To.UTC().Format(dateFormat)
	}

	key := makeQueryCacheKey(queryTypeSavedReport, []byte(qm.ReportID), startDate, endDate)
	result, err := d.queries.do(ctx, key, func(queryContext context.Context) (cachedQueryResult, error) {
		resp, err := d.queryClient.RunReport(queryContext, qm.ReportID, startDate, endDate)
		if err != nil {
			return cachedQueryResult{}, err
		}

		name := resp.ReportName
		if name == "" {
			name = qm.ReportID
		}

		result := cachedQueryResult{name: name, result: resp.Result}
		if _, err := FramesFromResult(result.name, &result.result); err != nil {
			return cachedQueryResult{}, fmt.Errorf("transform result: %w", err)
		}

		return result, nil
	})
	if err != nil {
		return apiErrorResponse(err)
	}

	frames, err := framesFromResult(result.name, &result.result, qm.ResultFormat)
	if err != nil {
		return pluginErrorResponse(backend.StatusInternal, fmt.Sprintf("transform result: %v", err))
	}

	return backend.DataResponse{Frames: frames}
}

func framesFromResult(name string, result *doitapi.ReportResult, resultFormat string) (data.Frames, error) {
	if resultFormat == resultFormatTreemap {
		return TreemapFramesFromResult(name, result)
	}

	return FramesFromResult(name, result)
}

func (d *Datasource) queryAdHoc(ctx context.Context, qm queryModel) backend.DataResponse {
	if len(qm.Config) == 0 {
		return pluginErrorResponse(backend.StatusBadRequest, "query config is empty")
	}

	key, err := makeAdHocQueryCacheKey(qm.Config)
	if err != nil {
		return pluginErrorResponse(backend.StatusBadRequest, "query config is invalid")
	}

	result, err := d.queries.do(ctx, key, func(queryContext context.Context) (cachedQueryResult, error) {
		resp, err := d.queryClient.RunQuery(queryContext, qm.Config)
		if err != nil {
			return cachedQueryResult{}, err
		}

		result := cachedQueryResult{name: "query", result: resp.Result}
		if _, err := FramesFromResult(result.name, &result.result); err != nil {
			return cachedQueryResult{}, fmt.Errorf("transform result: %w", err)
		}

		return result, nil
	})
	if err != nil {
		return apiErrorResponse(err)
	}

	frames, err := FramesFromResult(result.name, &result.result)
	if err != nil {
		return pluginErrorResponse(backend.StatusInternal, fmt.Sprintf("transform result: %v", err))
	}

	return backend.DataResponse{Frames: frames}
}

func apiErrorResponse(err error) backend.DataResponse {
	var apiErr *doitapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusBadRequest:
			return downstreamErrorResponse(backend.StatusBadRequest, "Invalid query. Check the report or query configuration.")
		case http.StatusUnauthorized:
			return downstreamErrorResponse(backend.StatusUnauthorized, "Authentication failed. Check the DoiT API key.")
		case http.StatusForbidden:
			return downstreamErrorResponse(backend.StatusForbidden, "Access denied. Check the API key permissions.")
		case http.StatusNotFound:
			return downstreamErrorResponse(backend.StatusNotFound, "The report or query was not found.")
		case http.StatusRequestTimeout, http.StatusGatewayTimeout, 524:
			return downstreamErrorResponse(backend.StatusTimeout, "The report query timed out. Try a shorter time range.")
		case http.StatusTooManyRequests:
			return downstreamErrorResponse(backend.StatusTooManyRequests, "Too many report queries are running. Wait before refreshing.")
		default:
			if apiErr.StatusCode >= 500 {
				return downstreamErrorResponse(backend.StatusBadGateway, "The DoiT API is temporarily unavailable. Try again later.")
			}

			return downstreamErrorResponse(backend.StatusBadRequest, "The DoiT API rejected the query. Check its configuration.")
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return downstreamErrorResponse(backend.StatusTimeout, "The report query timed out. Try a shorter time range.")
	}

	if errors.Is(err, context.Canceled) {
		response := pluginErrorResponse(backend.StatusInternal, context.Canceled.Error())
		response.Error = err

		return response
	}

	if errors.Is(err, errQueryQueueFull) {
		return pluginErrorResponse(backend.StatusTooManyRequests, "Too many report queries are running. Wait before refreshing.")
	}

	if errors.Is(err, errCoordinatorClosed) {
		return pluginErrorResponse(backend.StatusInternal, "The data source is shutting down.")
	}

	var networkError net.Error
	if errors.As(err, &networkError) {
		return downstreamErrorResponse(backend.StatusBadGateway, "Could not reach the DoiT API. Try again.")
	}

	return pluginErrorResponse(backend.StatusInternal, "The query failed. Try again.")
}

func pluginErrorResponse(status backend.Status, message string) backend.DataResponse {
	return backend.ErrDataResponseWithSource(status, backend.ErrorSourcePlugin, message)
}

func downstreamErrorResponse(status backend.Status, message string) backend.DataResponse {
	return backend.ErrDataResponseWithSource(status, backend.ErrorSourceDownstream, message)
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

	if _, err := d.metadataClient.ListDimensions(ctx); err != nil {
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
		reports, err := d.metadataClient.ListReports(r.Context())
		if err != nil {
			writeResourceError(w, err)
			return
		}

		writeJSON(w, reports)
	})

	mux.HandleFunc("GET /dimensions", func(w http.ResponseWriter, r *http.Request) {
		dimensions, err := d.metadataClient.ListDimensions(r.Context())
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

		dimension, err := d.metadataClient.GetDimensionValues(r.Context(), dimensionType, dimensionID)
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
