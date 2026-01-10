package messaging

import (
	"project/internal/interfaces"
)

type MessageRouter struct {
	handlers map[string][]interfaces.MessageHandler
	fallback interfaces.MessageHandler
}

// Implementation methods
