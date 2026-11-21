package mattermost

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// Message types produced by ToMessage.
const (
	// TypeMessage is a post written by a user or another bot.
	TypeMessage = "message"
	// TypeSystemMessage is a post Mattermost writes itself, such as
	// "joined the channel"; its payload.post.type starts with "system_".
	TypeSystemMessage = "system_message"
	TypeReactionAdded = "reaction_added"
	// TypeReactionRemoved is emitted when a reaction is taken back.
	TypeReactionRemoved = "reaction_removed"
)

// Event is one frame of the Mattermost WebSocket event stream.
type Event struct {
	Event     string                 `json:"event"`
	Data      map[string]interface{} `json:"data"`
	Broadcast Broadcast              `json:"broadcast"`
	Seq       int64                  `json:"seq"`
}

// Broadcast describes who an event was sent to.
type Broadcast struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
	TeamID    string `json:"team_id"`
}

type reaction struct {
	UserID    string `json:"user_id"`
	PostID    string `json:"post_id"`
	EmojiName string `json:"emoji_name"`
	ChannelID string `json:"channel_id"`
	CreateAt  int64  `json:"create_at"`
}

// ToMessage converts an event into a rule engine message.
//
// It reports false, with no error, for events that are not routed (typing,
// status changes, and so on) and for the bot's own posts and reactions, so a
// bot never answers itself. An error means a routed event was malformed.
//
// A post becomes a message of type "message" (or "system_message") whose ID
// is the post ID, Content is the post text, and payload holds:
//
//	platform        "mattermost"
//	event           "posted"
//	post            id, root_id, thread_id (root_id, or id for a root post), is_reply, type, props
//	channel         id, name, display_name, type (public, private, direct, group), team_id
//	sender          id, username, is_bot
//	mentions        IDs of mentioned users; Mattermost counts the bot as mentioned in its direct messages
//	bot_mentioned   whether the bot is in mentions
//
// A reaction becomes "reaction_added" or "reaction_removed" with the emoji
// name as Content and payload.reaction, payload.post.id and payload.channel.id.
func ToMessage(event Event, bot User) (ruleengine.Message, bool, error) {
	switch event.Event {
	case "posted":
		return postedMessage(event, bot)
	case TypeReactionAdded, TypeReactionRemoved:
		return reactionMessage(event, bot)
	default:
		return ruleengine.Message{}, false, nil
	}
}

func postedMessage(event Event, bot User) (ruleengine.Message, bool, error) {
	var post Post
	if err := decodeEmbedded(event.Data, "post", &post); err != nil {
		return ruleengine.Message{}, false, err
	}
	if post.ID == "" {
		return ruleengine.Message{}, false, fmt.Errorf("posted event has no post id")
	}
	if bot.ID != "" && post.UserID == bot.ID {
		return ruleengine.Message{}, false, nil
	}

	var mentionIDs []string
	if _, present := event.Data["mentions"]; present {
		if err := decodeEmbedded(event.Data, "mentions", &mentionIDs); err != nil {
			return ruleengine.Message{}, false, err
		}
	}
	mentions := make([]interface{}, 0, len(mentionIDs))
	botMentioned := false
	for _, id := range mentionIDs {
		mentions = append(mentions, id)
		if id == bot.ID {
			botMentioned = true
		}
	}

	props := post.Props
	if props == nil {
		props = map[string]interface{}{}
	}
	threadID := post.RootID
	if threadID == "" {
		threadID = post.ID
	}
	channelID := post.ChannelID
	if channelID == "" {
		channelID = event.Broadcast.ChannelID
	}

	messageType := TypeMessage
	if strings.HasPrefix(post.Type, "system_") {
		messageType = TypeSystemMessage
	}

	return ruleengine.Message{
		ID:        post.ID,
		Type:      messageType,
		Content:   post.Message,
		UserID:    post.UserID,
		Timestamp: millisToTime(post.CreateAt),
		Payload: map[string]interface{}{
			"platform": "mattermost",
			"event":    event.Event,
			"post": map[string]interface{}{
				"id":        post.ID,
				"root_id":   post.RootID,
				"thread_id": threadID,
				"is_reply":  post.RootID != "",
				"type":      post.Type,
				"props":     props,
			},
			"channel": map[string]interface{}{
				"id":           channelID,
				"name":         stringValue(event.Data, "channel_name"),
				"display_name": stringValue(event.Data, "channel_display_name"),
				"type":         channelKind(stringValue(event.Data, "channel_type")),
				"team_id":      stringValue(event.Data, "team_id"),
			},
			"sender": map[string]interface{}{
				"id":       post.UserID,
				"username": strings.TrimPrefix(stringValue(event.Data, "sender_name"), "@"),
				"is_bot":   fmt.Sprint(props["from_bot"]) == "true",
			},
			"mentions":      mentions,
			"bot_mentioned": botMentioned,
		},
	}, true, nil
}

func reactionMessage(event Event, bot User) (ruleengine.Message, bool, error) {
	var r reaction
	if err := decodeEmbedded(event.Data, "reaction", &r); err != nil {
		return ruleengine.Message{}, false, err
	}
	if r.PostID == "" || r.EmojiName == "" {
		return ruleengine.Message{}, false, fmt.Errorf("%s event has no post id or emoji", event.Event)
	}
	if bot.ID != "" && r.UserID == bot.ID {
		return ruleengine.Message{}, false, nil
	}
	channelID := r.ChannelID
	if channelID == "" {
		channelID = event.Broadcast.ChannelID
	}
	return ruleengine.Message{
		ID:        fmt.Sprintf("%s:%s:%s:%s:%d", event.Event, r.PostID, r.UserID, r.EmojiName, r.CreateAt),
		Type:      event.Event,
		Content:   r.EmojiName,
		UserID:    r.UserID,
		Timestamp: millisToTime(r.CreateAt),
		Payload: map[string]interface{}{
			"platform": "mattermost",
			"event":    event.Event,
			"reaction": r.EmojiName,
			"post":     map[string]interface{}{"id": r.PostID},
			"channel":  map[string]interface{}{"id": channelID},
			"sender":   map[string]interface{}{"id": r.UserID},
		},
	}, true, nil
}

// decodeEmbedded decodes data[key], which Mattermost sends as a JSON string
// (and tests sometimes as a plain JSON value), into out.
func decodeEmbedded(data map[string]interface{}, key string, out interface{}) error {
	raw, present := data[key]
	if !present {
		return fmt.Errorf("event has no %s", key)
	}
	var encoded []byte
	switch value := raw.(type) {
	case string:
		encoded = []byte(value)
	default:
		var err error
		if encoded, err = json.Marshal(value); err != nil {
			return fmt.Errorf("event %s: %w", key, err)
		}
	}
	if err := json.Unmarshal(encoded, out); err != nil {
		return fmt.Errorf("event %s is not valid: %w", key, err)
	}
	return nil
}

func stringValue(data map[string]interface{}, key string) string {
	value, _ := data[key].(string)
	return value
}

// channelKind maps a Mattermost channel type letter to a readable name.
func channelKind(letter string) string {
	switch letter {
	case "O":
		return "public"
	case "P":
		return "private"
	case "D":
		return "direct"
	case "G":
		return "group"
	default:
		return letter
	}
}

func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(ms).UTC()
}
