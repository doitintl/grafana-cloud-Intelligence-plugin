package plugin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

const timestampFieldType = "timestamp"

// FramesFromResult converts a DoiT report result into Grafana data frames.
// Results with a trailing timestamp column become one wide time-series frame
// (one field per unique dimension combination); everything else becomes a table frame.
func FramesFromResult(name string, result *doitapi.ReportResult) (data.Frames, error) {
	if result == nil || len(result.Schema) == 0 {
		return data.Frames{data.NewFrame(name)}, nil
	}

	tsIdx := timestampIndex(result.Schema)
	if tsIdx >= 0 {
		frame, err := timeSeriesFrame(name, result, tsIdx)
		if err != nil {
			return nil, err
		}

		return data.Frames{frame}, nil
	}

	frame, err := tableFrame(name, result)
	if err != nil {
		return nil, err
	}

	return data.Frames{frame}, nil
}

func timestampIndex(schema []doitapi.SchemaField) int {
	for i, field := range schema {
		if field.Type == timestampFieldType {
			return i
		}
	}

	return -1
}

func timeSeriesFrame(name string, result *doitapi.ReportResult, tsIdx int) (*data.Frame, error) {
	metricIdxs, dimensionIdxs := classifyColumns(result.Schema, tsIdx)

	type seriesKey struct {
		metric int
		labels string
	}

	timestamps := make([]time.Time, 0)
	tsSeen := make(map[int64]int)
	series := make(map[seriesKey][]*float64)
	seriesLabels := make(map[seriesKey]data.Labels)
	order := make([]seriesKey, 0)

	for _, row := range result.Rows {
		if tsIdx >= len(row) {
			continue
		}

		ts, err := parseTimestamp(row[tsIdx])
		if err != nil {
			return nil, err
		}

		tsPos, ok := tsSeen[ts.UnixMilli()]
		if !ok {
			tsPos = len(timestamps)
			tsSeen[ts.UnixMilli()] = tsPos

			timestamps = append(timestamps, ts)
			for key := range series {
				series[key] = append(series[key], nil)
			}
		}

		labels := data.Labels{}
		labelParts := make([]string, 0, len(dimensionIdxs))

		for _, di := range dimensionIdxs {
			if di >= len(row) {
				continue
			}

			value := rawToString(row[di])
			labels[result.Schema[di].Name] = value
			labelParts = append(labelParts, value)
		}

		for _, mi := range metricIdxs {
			if mi >= len(row) {
				continue
			}

			key := seriesKey{metric: mi, labels: strings.Join(labelParts, "|")}

			if _, ok := series[key]; !ok {
				series[key] = make([]*float64, len(timestamps))
				seriesLabels[key] = labels
				order = append(order, key)
			}

			values := series[key]
			for len(values) < len(timestamps) {
				values = append(values, nil)
			}

			if v, err := rawToFloat(row[mi]); err == nil {
				values[tsPos] = &v
			}

			series[key] = values
		}
	}

	frame := data.NewFrame(name, data.NewField("time", nil, timestamps))

	for _, key := range order {
		values := series[key]
		for len(values) < len(timestamps) {
			values = append(values, nil)
		}

		field := data.NewField(result.Schema[key.metric].Name, seriesLabels[key], values)
		frame.Fields = append(frame.Fields, field)
	}

	return frame, nil
}

func tableFrame(name string, result *doitapi.ReportResult) (*data.Frame, error) {
	frame := data.NewFrame(name)

	for colIdx, schemaField := range result.Schema {
		switch schemaField.Type {
		case "float", "integer", "int":
			values := make([]*float64, len(result.Rows))

			for rowIdx, row := range result.Rows {
				if colIdx >= len(row) {
					continue
				}

				if v, err := rawToFloat(row[colIdx]); err == nil {
					values[rowIdx] = &v
				}
			}

			frame.Fields = append(frame.Fields, data.NewField(schemaField.Name, nil, values))
		default:
			values := make([]*string, len(result.Rows))

			for rowIdx, row := range result.Rows {
				if colIdx >= len(row) {
					continue
				}

				v := rawToString(row[colIdx])
				values[rowIdx] = &v
			}

			frame.Fields = append(frame.Fields, data.NewField(schemaField.Name, nil, values))
		}
	}

	return frame, nil
}

// datePartColumns are string columns emitted alongside the timestamp column for
// time-interval grouping; the timestamp already encodes them, so they must not
// become series labels.
var datePartColumns = map[string]bool{
	"year":          true,
	"quarter":       true,
	"month":         true,
	"week":          true,
	"iso_week":      true,
	"day":           true,
	"day_of_week":   true,
	"hour":          true,
	"week_day":      true,
	"year_week":     true,
	"year_month":    true,
	"month_day":     true,
	"date":          true,
	"datetime":      true,
	"usage_date":    true,
	"invoice_month": true,
}

func classifyColumns(schema []doitapi.SchemaField, tsIdx int) (metricIdxs, dimensionIdxs []int) {
	for i, field := range schema {
		if i == tsIdx {
			continue
		}

		switch field.Type {
		case "float", "integer", "int":
			metricIdxs = append(metricIdxs, i)
		case timestampFieldType:
			// extra timestamp columns are ignored
		default:
			if datePartColumns[strings.ToLower(field.Name)] {
				continue
			}

			dimensionIdxs = append(dimensionIdxs, i)
		}
	}

	return metricIdxs, dimensionIdxs
}

func parseTimestamp(raw json.RawMessage) (time.Time, error) {
	var seconds float64
	if err := json.Unmarshal(raw, &seconds); err == nil {
		return time.Unix(int64(seconds), 0).UTC(), nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if ts, err := time.Parse(time.RFC3339, str); err == nil {
			return ts, nil
		}

		if seconds, err := strconv.ParseFloat(str, 64); err == nil {
			return time.Unix(int64(seconds), 0).UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse timestamp value: %s", string(raw))
}

func rawToFloat(raw json.RawMessage) (float64, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strconv.ParseFloat(s, 64)
	}

	return 0, fmt.Errorf("cannot parse numeric value: %s", string(raw))
}

func rawToString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return strings.Trim(string(raw), `"`)
}
