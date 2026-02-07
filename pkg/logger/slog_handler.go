package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

func newHandler(out io.Writer, cfg Config) slog.Handler {
	if strings.EqualFold(cfg.Format, "json") {
		return &orderedJSONHandler{
			out:        out,
			level:      levelVar,
			timeFormat: cfg.TimeFormat,
			mu:         &sync.Mutex{},
		}
	}

	return slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.LevelKey:
				if level, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(strings.ToLower(level.String()))
				}
			case slog.TimeKey:
				if t := a.Value.Time(); !t.IsZero() && cfg.TimeFormat != "" {
					a.Value = slog.StringValue(t.Format(cfg.TimeFormat))
				}
			}
			return a
		},
	})
}

type orderedJSONHandler struct {
	out        io.Writer
	level      slog.Leveler
	timeFormat string
	attrs      []slog.Attr
	group      string
	mu         *sync.Mutex
}

func (h *orderedJSONHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *orderedJSONHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make(map[string]interface{}, r.NumAttrs()+len(h.attrs)+8)

	for _, attr := range h.attrs {
		h.addAttr(fields, attr, h.group)
	}
	r.Attrs(func(attr slog.Attr) bool {
		h.addAttr(fields, attr, h.group)
		return true
	})

	timestamp := r.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timeFormat := h.timeFormat
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	orderedKeys := []string{
		"time",
		"level",
		"msg",
		"service",
		"operation",
		"request_id",
		"trace_id",
		"user_id",
		"duration_ms",
		"elapsed_ms",
		"success",
		"error",
		"file",
		"func",
	}

	payload := map[string]interface{}{
		"time":  timestamp.Format(timeFormat),
		"level": strings.ToLower(r.Level.String()),
		"msg":   r.Message,
	}

	for k, v := range fields {
		payload[k] = v
	}

	var rest []string
	seen := make(map[string]struct{}, len(orderedKeys))
	for _, key := range orderedKeys {
		seen[key] = struct{}{}
	}
	for key := range payload {
		if _, ok := seen[key]; ok {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)

	buf := &bytes.Buffer{}
	buf.WriteByte('{')

	writeKV := func(key string, val interface{}, first *bool) error {
		if !*first {
			buf.WriteByte(',')
		}
		*first = false

		kb, _ := json.Marshal(key)
		vb, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
		return nil
	}

	first := true
	for _, key := range orderedKeys {
		val, ok := payload[key]
		if !ok {
			continue
		}
		if err := writeKV(key, val, &first); err != nil {
			return err
		}
	}
	for _, key := range rest {
		if err := writeKV(key, payload[key], &first); err != nil {
			return err
		}
	}

	buf.WriteByte('}')
	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(buf.Bytes())
	return err
}

func (h *orderedJSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &out
}

func (h *orderedJSONHandler) WithGroup(name string) slog.Handler {
	out := *h
	if out.group == "" {
		out.group = name
	} else {
		out.group = out.group + "." + name
	}
	return &out
}

func (h *orderedJSONHandler) addAttr(fields map[string]interface{}, attr slog.Attr, groupPrefix string) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}

	key := attr.Key
	if groupPrefix != "" && key != "" {
		key = groupPrefix + "." + key
	}

	switch attr.Value.Kind() {
	case slog.KindGroup:
		subGroup := groupPrefix
		if attr.Key != "" {
			if subGroup == "" {
				subGroup = attr.Key
			} else {
				subGroup = subGroup + "." + attr.Key
			}
		}
		for _, ga := range attr.Value.Group() {
			h.addAttr(fields, ga, subGroup)
		}
	default:
		fields[key] = slogValueToAny(attr.Value)
	}
}

func slogValueToAny(v slog.Value) interface{} {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindAny:
		any := v.Any()
		if err, ok := any.(error); ok {
			return err.Error()
		}
		return any
	default:
		return fmt.Sprint(v)
	}
}
