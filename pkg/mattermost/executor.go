package mattermost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// Executor types registered by this package.
const (
	PostExecutorType     = "mattermost_post"
	ReactionExecutorType = "mattermost_reaction"
)

// PostExecutor posts a message as the bot.
//
// Params:
//
//	template    message text, rendered with executor.RenderTemplate (required)
//	channel_id  where to post; defaults to the event's payload.channel.id
//	thread      true to reply in the thread of the triggering post
//
// Retries are safe: each post carries a pending_post_id derived from the
// event ID, action ID and channel, so Mattermost returns the first post
// instead of posting again.
type PostExecutor struct {
	client *Client
}

// NewPostExecutor returns a PostExecutor that posts through client.
func NewPostExecutor(client *Client) *PostExecutor {
	return &PostExecutor{client: client}
}

// Type returns "mattermost_post".
func (e *PostExecutor) Type() string { return PostExecutorType }

// Execute renders the template and creates the post.
func (e *PostExecutor) Execute(ctx context.Context, message ruleengine.Message, action ruleengine.Action) error {
	tmpl, _ := action.Params["template"].(string)
	if tmpl == "" {
		return executor.Permanent(errors.New("mattermost_post requires params.template"))
	}
	text, err := executor.RenderTemplate(tmpl, message)
	if err != nil {
		return executor.Permanent(fmt.Errorf("render mattermost_post template: %w", err))
	}

	channelID, _ := action.Params["channel_id"].(string)
	if channelID == "" {
		channelID = payloadString(message, "payload.channel.id")
	}
	if channelID == "" {
		return executor.Permanent(errors.New("mattermost_post needs params.channel_id or an event with payload.channel.id"))
	}

	post := Post{
		ChannelID:     channelID,
		Message:       text,
		PendingPostID: pendingPostID(message.ID, action.ID, channelID),
	}
	if thread, _ := action.Params["thread"].(bool); thread {
		rootID, err := e.threadRoot(ctx, message)
		if err != nil {
			return err
		}
		post.RootID = rootID
	}
	_, err = e.client.CreatePost(ctx, post)
	return err
}

// threadRoot returns the root post of the thread the event belongs to. Post
// events carry it; for a reaction, the reacted-to post is looked up.
func (e *PostExecutor) threadRoot(ctx context.Context, message ruleengine.Message) (string, error) {
	if threadID := payloadString(message, "payload.post.thread_id"); threadID != "" {
		return threadID, nil
	}
	postID := payloadString(message, "payload.post.id")
	if postID == "" {
		return "", executor.Permanent(errors.New("mattermost_post thread reply needs an event with payload.post.id"))
	}
	post, err := e.client.GetPost(ctx, postID)
	if err != nil {
		return "", err
	}
	if post.RootID != "" {
		return post.RootID, nil
	}
	return post.ID, nil
}

// ReactionExecutor adds an emoji reaction as the bot.
//
// Params:
//
//	emoji    emoji name such as "white_check_mark" (required)
//	post_id  post to react to; defaults to the event's payload.post.id
type ReactionExecutor struct {
	client *Client
}

// NewReactionExecutor returns a ReactionExecutor that reacts through client.
func NewReactionExecutor(client *Client) *ReactionExecutor {
	return &ReactionExecutor{client: client}
}

// Type returns "mattermost_reaction".
func (e *ReactionExecutor) Type() string { return ReactionExecutorType }

// Execute adds the reaction.
func (e *ReactionExecutor) Execute(ctx context.Context, message ruleengine.Message, action ruleengine.Action) error {
	emoji, _ := action.Params["emoji"].(string)
	if emoji == "" {
		return executor.Permanent(errors.New("mattermost_reaction requires params.emoji"))
	}
	postID, _ := action.Params["post_id"].(string)
	if postID == "" {
		postID = payloadString(message, "payload.post.id")
	}
	if postID == "" {
		return executor.Permanent(errors.New("mattermost_reaction needs params.post_id or an event with payload.post.id"))
	}
	return e.client.AddReaction(ctx, postID, emoji)
}

func payloadString(message ruleengine.Message, path string) string {
	value, ok := message.Field(path)
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// pendingPostID is stable for one action on one event, so retries of that
// action are recognized by Mattermost as the same post.
func pendingPostID(messageID, actionID, channelID string) string {
	if messageID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(messageID + "\x00" + actionID + "\x00" + channelID))
	return "gsp-" + hex.EncodeToString(sum[:16])
}
