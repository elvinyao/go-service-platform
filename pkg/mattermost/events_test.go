package mattermost

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

var testBot = User{ID: testBotID, Username: "gsp-bot", IsBot: true}

func decodeTestEvent(t *testing.T, raw map[string]interface{}) Event {
	t.Helper()
	data, _ := json.Marshal(raw)
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event
}

func field(t *testing.T, message ruleengine.Message, path string) interface{} {
	t.Helper()
	value, ok := message.Field(path)
	if !ok {
		t.Fatalf("field %s is absent", path)
	}
	return value
}

func TestToMessageConvertsRootPost(t *testing.T) {
	post := Post{ID: "p1", ChannelID: testChannelID, UserID: "alice-id", Message: "hello @gsp-bot !ping", CreateAt: 1795266862870}
	event := decodeTestEvent(t, postedEvent(2, post, []string{testBotID}))

	message, routed, err := ToMessage(event, testBot)
	if err != nil || !routed {
		t.Fatalf("routed = %v, err = %v", routed, err)
	}
	if message.ID != "p1" || message.Type != TypeMessage || message.Content != post.Message || message.UserID != "alice-id" {
		t.Fatalf("message = %+v", message)
	}
	if !message.Timestamp.Equal(time.UnixMilli(1795266862870)) {
		t.Fatalf("timestamp = %v", message.Timestamp)
	}
	want := map[string]interface{}{
		"payload.platform":             "mattermost",
		"payload.event":                "posted",
		"payload.post.thread_id":       "p1",
		"payload.post.is_reply":        false,
		"payload.channel.id":           testChannelID,
		"payload.channel.name":         "bot-test",
		"payload.channel.display_name": "Bot Test",
		"payload.channel.type":         "public",
		"payload.channel.team_id":      "team-1",
		"payload.sender.username":      "alice",
		"payload.sender.is_bot":        false,
		"payload.bot_mentioned":        true,
		"payload.mentions.0":           testBotID,
	}
	for path, value := range want {
		if got := field(t, message, path); got != value {
			t.Fatalf("%s = %v, want %v", path, got, value)
		}
	}
	// The engine's contains operator checks list membership.
	matched := ruleengine.MatchCondition(ruleengine.Condition{Field: "payload.mentions", Op: ruleengine.OpContains, Value: testBotID}, message)
	if !matched {
		t.Fatalf("mentions list is not usable in conditions")
	}
}

func TestToMessageConvertsRepliesSystemPostsAndOtherBots(t *testing.T) {
	reply := Post{ID: "p2", ChannelID: testChannelID, UserID: "alice-id", RootID: "p1", Message: "a reply", Props: map[string]interface{}{"from_bot": "true"}}
	message, _, err := ToMessage(decodeTestEvent(t, postedEvent(3, reply, nil)), testBot)
	if err != nil {
		t.Fatalf("convert reply: %v", err)
	}
	if field(t, message, "payload.post.thread_id") != "p1" || field(t, message, "payload.post.is_reply") != true {
		t.Fatalf("reply thread fields wrong: %v", message.Payload["post"])
	}
	if field(t, message, "payload.bot_mentioned") != false || len(message.Payload["mentions"].([]interface{})) != 0 {
		t.Fatalf("a post without mentions should have an empty list")
	}
	if field(t, message, "payload.sender.is_bot") != true {
		t.Fatalf("from_bot prop should mark the sender as a bot")
	}

	system := Post{ID: "p3", ChannelID: testChannelID, UserID: "alice-id", Type: "system_join_channel"}
	message, _, _ = ToMessage(decodeTestEvent(t, postedEvent(4, system, nil)), testBot)
	if message.Type != TypeSystemMessage {
		t.Fatalf("system post type = %s", message.Type)
	}
}

func TestToMessageSkipsOwnActivityAndUnroutedEvents(t *testing.T) {
	own := Post{ID: "p4", ChannelID: testChannelID, UserID: testBotID, Message: "my reply"}
	if _, routed, err := ToMessage(decodeTestEvent(t, postedEvent(5, own, nil)), testBot); routed || err != nil {
		t.Fatalf("own post routed = %v, err = %v", routed, err)
	}
	if _, routed, err := ToMessage(decodeTestEvent(t, reactionEvent(6, TypeReactionAdded, testBotID, "p1", "eyes")), testBot); routed || err != nil {
		t.Fatalf("own reaction routed = %v, err = %v", routed, err)
	}
	for _, name := range []string{"typing", "status_change", "hello"} {
		if _, routed, err := ToMessage(Event{Event: name}, testBot); routed || err != nil {
			t.Fatalf("%s routed = %v, err = %v", name, routed, err)
		}
	}
}

func TestToMessageConvertsReactions(t *testing.T) {
	for _, name := range []string{TypeReactionAdded, TypeReactionRemoved} {
		message, routed, err := ToMessage(decodeTestEvent(t, reactionEvent(7, name, "alice-id", "p1", "+1")), testBot)
		if err != nil || !routed {
			t.Fatalf("%s routed = %v, err = %v", name, routed, err)
		}
		if message.Type != name || message.Content != "+1" || message.UserID != "alice-id" || message.ID == "" {
			t.Fatalf("message = %+v", message)
		}
		if field(t, message, "payload.reaction") != "+1" || field(t, message, "payload.post.id") != "p1" || field(t, message, "payload.channel.id") != testChannelID {
			t.Fatalf("payload = %v", message.Payload)
		}
	}

	// The channel falls back to the broadcast when the reaction omits it.
	encoded, _ := json.Marshal(map[string]string{"user_id": "u", "post_id": "p1", "emoji_name": "eyes"})
	event := Event{Event: TypeReactionAdded, Data: map[string]interface{}{"reaction": string(encoded)}, Broadcast: Broadcast{ChannelID: "from-broadcast"}}
	message, _, _ := ToMessage(event, testBot)
	if field(t, message, "payload.channel.id") != "from-broadcast" {
		t.Fatalf("channel fallback failed: %v", message.Payload["channel"])
	}
}

func TestToMessageRejectsMalformedEvents(t *testing.T) {
	cases := map[string]Event{
		"posted without post":    {Event: "posted", Data: map[string]interface{}{}},
		"post is not JSON":       {Event: "posted", Data: map[string]interface{}{"post": "{"}},
		"post without id":        {Event: "posted", Data: map[string]interface{}{"post": `{"message":"x"}`}},
		"mentions are not JSON":  {Event: "posted", Data: map[string]interface{}{"post": `{"id":"p"}`, "mentions": "nope"}},
		"reaction without emoji": {Event: TypeReactionAdded, Data: map[string]interface{}{"reaction": `{"post_id":"p"}`}},
		"reaction not decodable": {Event: TypeReactionAdded, Data: map[string]interface{}{"reaction": 42}},
		"embedded value invalid": {Event: "posted", Data: map[string]interface{}{"post": map[string]interface{}{"bad": func() {}}}},
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ToMessage(event, testBot); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}

	// Tests and other producers may embed objects instead of JSON strings.
	event := Event{Event: "posted", Data: map[string]interface{}{"post": map[string]interface{}{"id": "p9", "channel_id": "c"}}}
	if message, routed, err := ToMessage(event, testBot); err != nil || !routed || message.ID != "p9" {
		t.Fatalf("object-embedded post = %+v, %v, %v", message, routed, err)
	}
}

func TestChannelKindAndTimestampFallback(t *testing.T) {
	for letter, want := range map[string]string{"O": "public", "P": "private", "D": "direct", "G": "group", "X": "X"} {
		if got := channelKind(letter); got != want {
			t.Fatalf("channelKind(%s) = %s, want %s", letter, got, want)
		}
	}
	if ts := millisToTime(0); time.Since(ts) > time.Minute {
		t.Fatalf("zero timestamp should fall back to now, got %v", ts)
	}
}
