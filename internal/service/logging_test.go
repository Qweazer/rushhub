package service

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"
)

type capturedLog struct {
	message string
	attrs   map[string]any
}

type capturedLogs struct {
	mu      sync.Mutex
	records []capturedLog
}

func captureServiceLogs(t *testing.T) *capturedLogs {
	t.Helper()
	logs := &capturedLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(&captureLogHandler{logs: logs}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func (l *capturedLogs) requireShopEvent(t *testing.T, operation, key string, shopID uint64) {
	t.Helper()
	record, ok := l.find(operation, key)
	if !ok {
		t.Fatalf("missing %s log for key %q", operation, key)
	}
	if !nonEmptyValue(record.attrs["error"]) {
		t.Fatalf("%s log for key %q has no non-empty error: %#v", operation, key, record.attrs)
	}
	if got, ok := unsignedValue(record.attrs["shop_id"]); !ok || got != shopID {
		t.Fatalf("%s log shop_id = %#v, want %d", operation, record.attrs["shop_id"], shopID)
	}
}

func (l *capturedLogs) requireBatchEvent(t *testing.T, operation string, keys []string, shopIDs []uint64) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if record.attrs["operation"] != operation {
			continue
		}
		if !reflect.DeepEqual(record.attrs["key"], keys) {
			continue
		}
		if !nonEmptyValue(record.attrs["error"]) {
			t.Fatalf("%s batch log has no non-empty error: %#v", operation, record.attrs)
		}
		if !equalUnsignedSlice(record.attrs["shop_ids"], shopIDs) {
			t.Fatalf("%s batch log shop_ids = %#v, want %#v", operation, record.attrs["shop_ids"], shopIDs)
		}
		return
	}
	t.Fatalf("missing %s batch log for keys %#v", operation, keys)
}

func (l *capturedLogs) find(operation, key string) (capturedLog, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if record.attrs["operation"] == operation && record.attrs["key"] == key {
			return record, true
		}
	}
	return capturedLog{}, false
}

func nonEmptyValue(value any) bool {
	return value != nil && fmt.Sprint(value) != ""
}

func unsignedValue(value any) (uint64, bool) {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return 0, false
	}
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), true
	default:
		return 0, false
	}
}

func equalUnsignedSlice(value any, want []uint64) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Len() != len(want) {
		return false
	}
	for i := range want {
		got, ok := unsignedValue(v.Index(i).Interface())
		if !ok || got != want[i] {
			return false
		}
	}
	return true
}

type captureLogHandler struct {
	logs  *capturedLogs
	attrs []slog.Attr
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		attrs[attr.Key] = attr.Value.Resolve().Any()
	}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Resolve().Any()
		return true
	})
	h.logs.mu.Lock()
	h.logs.records = append(h.logs.records, capturedLog{message: record.Message, attrs: attrs})
	h.logs.mu.Unlock()
	return nil
}

func (h *captureLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	all := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	all = append(all, h.attrs...)
	all = append(all, attrs...)
	return &captureLogHandler{logs: h.logs, attrs: all}
}

func (h *captureLogHandler) WithGroup(string) slog.Handler { return h }
