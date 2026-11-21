package executor

import (
	"bytes"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// RenderTemplate renders tmpl with Go text/template against msg.
//
// The data exposes .ID, .Type, .Content, .UserID, .Timestamp, .Metadata and
// .Payload. A reference to a map key that is not present, such as
// {{.Payload.channel}} on a message without that key, is an error rather than
// the text "<no value>"; use the field helper for optional values.
//
// Helpers:
//   - json VALUE renders VALUE as JSON, quoting and escaping strings, so user
//     text can be placed in a JSON body safely: {"text": {{json .Content}}}.
//   - field PATH returns the value at a condition path such as
//     "payload.thread.id", or "" when it is absent.
//   - default FALLBACK VALUE returns FALLBACK when VALUE is nil or "":
//     {{field "payload.channel.name" | default "unknown"}}.
func RenderTemplate(tmpl string, msg ruleengine.Message) (string, error) {
	if tmpl == "" {
		return "", nil
	}

	t, err := template.New("executor").
		Option("missingkey=error").
		Funcs(templateFuncs(msg)).
		Parse(tmpl)
	if err != nil {
		return "", err
	}

	data := map[string]interface{}{
		"ID":        msg.ID,
		"Type":      msg.Type,
		"Content":   msg.Content,
		"UserID":    msg.UserID,
		"Timestamp": msg.Timestamp,
		"Metadata":  msg.Metadata,
		"Payload":   msg.Payload,
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func templateFuncs(msg ruleengine.Message) template.FuncMap {
	return template.FuncMap{
		"json": templateJSON,
		"field": func(path string) interface{} {
			value, ok := msg.Field(path)
			if !ok {
				return ""
			}
			return value
		},
		"default": func(fallback, value interface{}) interface{} {
			if value == nil {
				return fallback
			}
			if text, ok := value.(string); ok && text == "" {
				return fallback
			}
			return value
		},
	}
}

// templateJSON encodes value as compact JSON without HTML escaping, so chat
// markup such as "<@user>" stays readable.
func templateJSON(value interface{}) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
