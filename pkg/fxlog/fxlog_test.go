package fxlog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/fxlog"
	"github.com/rs/zerolog"
	"go.uber.org/fx/fxevent"
)

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(raw)
	if idx := strings.LastIndex(line, "\n"); idx >= 0 {
		line = line[idx+1:]
	}
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("not JSON: %q: %v", line, err)
	}
	return fields
}

func TestLogEventEmitsStructuredJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := fxlog.New(zerolog.New(&buf))

	logger.LogEvent(&fxevent.Provided{
		ConstructorName: "echopoint/internal/feature/flows.NewHandler()",
		OutputTypeNames: []string{"*flows.Handler"},
		ModuleName:      "flows",
	})

	fields := decode(t, buf.String())
	if fields["message"] != "provided" {
		t.Fatalf("message = %v, want provided", fields["message"])
	}
	if fields["component"] != "fx" {
		t.Fatalf("component = %v, want fx", fields["component"])
	}
	if fields["module"] != "flows" {
		t.Fatalf("module = %v, want flows", fields["module"])
	}
	if fields["constructor"] != "echopoint/internal/feature/flows.NewHandler()" {
		t.Fatalf("constructor = %v", fields["constructor"])
	}
}

func TestLogEventLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     fxevent.Event
		wantLevel string
		wantMsg   string
	}{
		{
			name:      "started milestone is info",
			event:     &fxevent.Started{},
			wantLevel: "info",
			wantMsg:   "started",
		},
		{
			name:      "provided is debug",
			event:     &fxevent.Provided{OutputTypeNames: []string{"X"}},
			wantLevel: "debug",
			wantMsg:   "provided",
		},
		{
			name:      "start failure is error",
			event:     &fxevent.Started{Err: errors.New("boom")},
			wantLevel: "error",
			wantMsg:   "start failed",
		},
		{
			name:      "invoke failure is error",
			event:     &fxevent.Invoked{FunctionName: "f", Err: errors.New("boom")},
			wantLevel: "error",
			wantMsg:   "invoke failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			fxlog.New(zerolog.New(&buf)).LogEvent(tt.event)

			fields := decode(t, buf.String())
			if fields["level"] != tt.wantLevel {
				t.Fatalf("level = %v, want %v", fields["level"], tt.wantLevel)
			}
			if fields["message"] != tt.wantMsg {
				t.Fatalf("message = %v, want %v", fields["message"], tt.wantMsg)
			}
		})
	}
}
