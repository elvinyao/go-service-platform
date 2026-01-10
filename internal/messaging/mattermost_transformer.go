package messaging

import (
	"project/internal/config"
	"text/template"
)

type MattermostMessageTransformer struct {
	templates map[string]*template.Template
	config    config.MattermostConfig
}

// Implementation methods
