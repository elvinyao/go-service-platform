package executor

import (
	"bytes"
	"text/template"

	"project/internal/model"
)

func renderTemplate(tmpl string, msg model.Message) (string, error) {
	if tmpl == "" {
		return "", nil
	}

	t, err := template.New("executor").Parse(tmpl)
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
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}
