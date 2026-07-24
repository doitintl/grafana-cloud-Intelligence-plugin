package doitapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	reportsPath    = "/analytics/v1/reports"
	queryPath      = "/analytics/v1/reports/query"
	dimensionsPath = "/analytics/v1/dimensions"
	dimensionPath  = "/analytics/v1/dimension"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

type SchemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type ReportResult struct {
	Schema       []SchemaField       `json:"schema"`
	Rows         [][]json.RawMessage `json:"rows"`
	ForecastRows [][]json.RawMessage `json:"forecastRows"`
}

type RunReportResponse struct {
	ID         string       `json:"id"`
	ReportName string       `json:"reportName"`
	URL        string       `json:"urlUI"`
	Result     ReportResult `json:"result"`
}

type RunQueryResponse struct {
	Result ReportResult `json:"result"`
}

type ReportListItem struct {
	ID         string `json:"id"`
	ReportName string `json:"reportName"`
	Owner      string `json:"owner"`
	Type       string `json:"type"`
}

type ReportsListResponse struct {
	Reports   []ReportListItem `json:"reports"`
	PageToken string           `json:"pageToken"`
}

type Dimension struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type DimensionsListResponse struct {
	Dimensions []Dimension `json:"dimensions"`
	PageToken  string      `json:"pageToken"`
}

type DimensionValue struct {
	Value string `json:"value"`
	Cloud string `json:"cloud"`
}

type DimensionResponse struct {
	ID     string           `json:"id"`
	Label  string           `json:"label"`
	Type   string           `json:"type"`
	Values []DimensionValue `json:"values"`
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("DoiT API error: status %d: %s", e.StatusCode, e.Body)
}

func (c *Client) RunReport(ctx context.Context, reportID, startDate, endDate string) (*RunReportResponse, error) {
	params := url.Values{}
	if startDate != "" && endDate != "" {
		params.Set("startDate", startDate)
		params.Set("endDate", endDate)
	}

	var resp RunReportResponse
	if err := c.get(ctx, reportsPath+"/"+url.PathEscape(reportID), params, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) RunQuery(ctx context.Context, config json.RawMessage) (*RunQueryResponse, error) {
	body, err := json.Marshal(map[string]json.RawMessage{"config": config})
	if err != nil {
		return nil, err
	}

	var resp RunQueryResponse
	if err := c.post(ctx, queryPath, body, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) ListReports(ctx context.Context) ([]ReportListItem, error) {
	var all []ReportListItem

	pageToken := ""

	for {
		params := url.Values{}
		params.Set("maxResults", "500")

		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		var resp ReportsListResponse
		if err := c.get(ctx, reportsPath, params, &resp); err != nil {
			return nil, err
		}

		all = append(all, resp.Reports...)

		if resp.PageToken == "" {
			break
		}

		pageToken = resp.PageToken
	}

	return all, nil
}

func (c *Client) ListDimensions(ctx context.Context) ([]Dimension, error) {
	var all []Dimension

	pageToken := ""

	for {
		params := url.Values{}
		params.Set("maxResults", "500")

		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		var resp DimensionsListResponse
		if err := c.get(ctx, dimensionsPath, params, &resp); err != nil {
			return nil, err
		}

		all = append(all, resp.Dimensions...)

		if resp.PageToken == "" {
			break
		}

		pageToken = resp.PageToken
	}

	return all, nil
}

func (c *Client) GetDimensionValues(ctx context.Context, dimensionType, dimensionID string) (*DimensionResponse, error) {
	params := url.Values{}
	params.Set("type", dimensionType)
	params.Set("id", dimensionID)

	var resp DimensionResponse
	if err := c.get(ctx, dimensionPath, params, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}

	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
