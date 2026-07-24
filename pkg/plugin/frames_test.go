package plugin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

func raw(v string) json.RawMessage {
	return json.RawMessage(v)
}

func TestFramesFromResult_TimeSeries(t *testing.T) {
	result := &doitapi.ReportResult{
		Schema: []doitapi.SchemaField{
			{Name: "service_description", Type: "string"},
			{Name: "cost", Type: "float"},
			{Name: "timestamp", Type: "timestamp"},
		},
		Rows: [][]json.RawMessage{
			{raw(`"BigQuery"`), raw(`10.5`), raw(`1743465600`)},
			{raw(`"Compute Engine"`), raw(`20`), raw(`1743465600`)},
			{raw(`"BigQuery"`), raw(`11.5`), raw(`1743552000`)},
		},
	}

	frames, err := FramesFromResult("test", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}

	frame := frames[0]

	// time field + one field per service
	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(frame.Fields))
	}

	timeField := frame.Fields[0]
	if timeField.Len() != 2 {
		t.Fatalf("expected 2 timestamps, got %d", timeField.Len())
	}

	firstTS, ok := timeField.At(0).(time.Time)
	if !ok || firstTS.Unix() != 1743465600 {
		t.Errorf("unexpected first timestamp: %v", timeField.At(0))
	}

	bqField := frame.Fields[1]
	if bqField.Labels["service_description"] != "BigQuery" {
		t.Errorf("unexpected labels: %v", bqField.Labels)
	}

	if v, ok := bqField.At(0).(*float64); !ok || v == nil || *v != 10.5 {
		t.Errorf("unexpected BigQuery value at t0: %v", bqField.At(0))
	}

	if v, ok := bqField.At(1).(*float64); !ok || v == nil || *v != 11.5 {
		t.Errorf("unexpected BigQuery value at t1: %v", bqField.At(1))
	}

	ceField := frame.Fields[2]
	if v, ok := ceField.At(1).(*float64); !ok || v != nil {
		t.Errorf("expected nil Compute Engine value at t1, got: %v", ceField.At(1))
	}
}

func TestFramesFromResult_Table(t *testing.T) {
	result := &doitapi.ReportResult{
		Schema: []doitapi.SchemaField{
			{Name: "service_description", Type: "string"},
			{Name: "cost", Type: "float"},
		},
		Rows: [][]json.RawMessage{
			{raw(`"BigQuery"`), raw(`10.5`)},
			{raw(`"Compute Engine"`), raw(`20`)},
		},
	}

	frames, err := FramesFromResult("test", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	frame := frames[0]

	if len(frame.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(frame.Fields))
	}

	if frame.Fields[0].Len() != 2 {
		t.Fatalf("expected 2 rows, got %d", frame.Fields[0].Len())
	}

	if v, ok := frame.Fields[0].At(0).(*string); !ok || v == nil || *v != "BigQuery" {
		t.Errorf("unexpected string value: %v", frame.Fields[0].At(0))
	}

	if v, ok := frame.Fields[1].At(1).(*float64); !ok || v == nil || *v != 20 {
		t.Errorf("unexpected float value: %v", frame.Fields[1].At(1))
	}
}

func TestFramesFromResult_Empty(t *testing.T) {
	frames, err := FramesFromResult("empty", &doitapi.ReportResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "unix seconds number", input: `1743465600`, want: 1743465600},
		{name: "unix seconds string", input: `"1743465600"`, want: 1743465600},
		{name: "rfc3339", input: `"2025-04-01T00:00:00Z"`, want: 1743465600},
		{name: "invalid", input: `"not-a-time"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimestamp(raw(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Unix() != tt.want {
				t.Errorf("got %d, want %d", got.Unix(), tt.want)
			}
		})
	}
}
