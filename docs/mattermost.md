# Mattermost Bots

`pkg/mattermost` connects the rule engine to Mattermost as a bot account. Posts and reactions in the bot's channels become rule engine messages; rules answer with the `mattermost_post` and `mattermost_reaction` executors.

This is the real integration. The older `adapters.mattermost` demo adapter, used by `make dev` against `fake-server`, only posts to one channel and is unrelated.

## Five-Minute Local Setup

Requirements: Docker with Compose, and Go.

```bash
make mattermost-up     # start Mattermost 12.1 + PostgreSQL, then provision it
make mattermost-bot    # run the reference runtime with the Mattermost profile
```

`make mattermost-up` runs `docker compose` with `deploy/mattermost/docker-compose.yml`, waits for the server, and then runs `go run ./deploy/mattermost/provision`, which creates or reuses:

| What | Value |
| --- | --- |
| Web UI | <http://localhost:8065/dev/channels/bot-test> |
| Admin login | `admin` / `Admin-Passw0rd!` |
| Team / channel | `dev` / `bot-test` |
| Bot | `@gsp-bot`, a member of the team and the channel |

It writes the bot token and IDs to `deploy/mattermost/.env` (ignored by git, mode 0600). Running it again reuses the token while it is still valid. Provisioning uses only the REST API: on a fresh server the first account becomes the system admin, so no other credentials are needed. Point it at another server with `go run ./deploy/mattermost/provision -url https://chat.example -admin-user ... -admin-password ...`.

Then log in, open `bot-test`, and try:

| You type | The bot |
| --- | --- |
| `!ping` | reacts with ✅ and replies `pong` in the thread |
| `!deploy api` | acknowledges in the thread |
| `@gsp-bot hello`, or any direct message to the bot | replies with usage help in the thread |
| a 👍 reaction on any post | logs it (see the runtime log) |

The rules live in `config/profiles/mattermost/workflow-rules.yaml`; restart `make mattermost-bot` after editing them.

Other targets: `make mattermost-down` stops the containers and keeps data; `make mattermost-reset` deletes all data and the `.env` file. Set `MATTERMOST_PORT` to use a port other than 8065. The compose file is for local development only: fixed passwords, no TLS, and local mode enabled.

## Runtime Configuration

```yaml
inputs:
  websocket:
    enabled: false
  mattermost:
    enabled: true
    server_url: "http://localhost:8065"
    reconnect_interval: "5s"
    ping_interval: "20s"
    read_timeout: "60s"
```

| Field / variable | Meaning |
| --- | --- |
| `server_url`, or `MATTERMOST_BOT_URL` | Mattermost base URL (`http` or `https`); the variable wins |
| `MATTERMOST_BOT_TOKEN` | Bot access token. Required when enabled. It is read only from the environment, never from YAML, and never appears in admin output |
| `reconnect_interval` | Delay between reconnect attempts |
| `ping_interval`, `read_timeout` | Keep-alive: ping this often; treat this much silence as a dead connection. Both must be positive, and the ping interval must be the shorter |

Startup fails if Mattermost rejects the token. If the server is merely unreachable, the runtime starts, keeps retrying, and reports `NOT_READY` until the event stream connects: the `mattermost-input` health check is critical.

In `config/profiles/mattermost/rule-engine.yaml`, `dispatch.ordering_key` is `payload.channel.id`: messages in one channel are handled in order, and different channels run in parallel.

## Messages

| Event | `type` | `id` | `content` | `user_id` |
| --- | --- | --- | --- | --- |
| A post | `message` (`system_message` for Mattermost's own posts such as "joined the channel") | post ID | post text | author |
| A reaction added / removed | `reaction_added` / `reaction_removed` | derived, unique per reaction | emoji name, such as `+1` | reacting user |

Payload of a post:

| Path | Example | Notes |
| --- | --- | --- |
| `payload.platform` | `mattermost` | |
| `payload.event` | `posted` | |
| `payload.post.id` / `.root_id` | | `root_id` is empty for a root post |
| `payload.post.thread_id` | | the thread's root post: `root_id`, or the post's own ID |
| `payload.post.is_reply` | `true` | |
| `payload.post.type`, `payload.post.props` | | raw Mattermost post type and props |
| `payload.channel.id` / `.name` / `.display_name` / `.team_id` | `bot-test` | |
| `payload.channel.type` | `public`, `private`, `direct`, `group` | |
| `payload.sender.id` / `.username` | `alice` | |
| `payload.sender.is_bot` | `false` | true for posts made by any bot or integration |
| `payload.mentions` | list of user IDs | use `contains` |
| `payload.bot_mentioned` | `true` | Mattermost also counts every direct message to the bot as a mention |

A reaction carries `payload.reaction` (emoji name), `payload.post.id`, `payload.channel` (filled in with a cached channel lookup), and `payload.sender.id`.

The bot never receives its own posts or reactions, so a reply that contains a command cannot trigger the bot again. Posts from other bots do arrive; add `payload.sender.is_bot eq false` to rules that must not answer bots, as the profile rules do.

Every routed event's `id` is unique, so the dispatcher's deduplication drops redelivered events.

## Executors

```yaml
actions:
  - id: ack
    executor: mattermost_reaction
    params:
      emoji: white_check_mark    # required; post_id defaults to payload.post.id
  - id: reply
    executor: mattermost_post
    retry:
      max_attempts: 3
      backoff: "500ms"
    params:
      thread: true               # reply in the triggering post's thread
      template: 'pong @{{field "payload.sender.username"}}'
      # channel_id: "..."        # defaults to payload.channel.id
```

- `mattermost_post` renders `template` with the usual template data and helpers (see [Executors](executors.md#templates)). The text is sent as JSON by the client, so quotes and line breaks in user content need no escaping.
- With `thread: true`, a reply to a post goes into that post's thread; for a reaction event, the reacted-to post is looked up to find its thread.
- Retrying `mattermost_post` is safe. Each post carries a `pending_post_id` derived from the event ID, action ID and channel, and Mattermost returns the existing post instead of posting twice.
- Configuration mistakes and 4xx responses (except 408 and 429) are permanent and not retried. Network errors, 408, 429 and 5xx responses are retried per the action's retry policy.

## Reliability

- The event stream sends pings every `ping_interval` and redials after `read_timeout` of silence, so a half-open connection does not go unnoticed.
- After a reconnect the listener asks Mattermost to replay the events it missed, by sending back the last `connection_id` and sequence number. This was verified against Mattermost 12.1: a message posted while the bot was disconnected was answered after it reconnected. If the server can no longer replay (after a long outage or a server restart), the runtime logs `could not resume the previous event stream`, and events from the gap are lost.
- Frames that are not valid events are counted (`malformed_events` on `GET /services`) and skipped without dropping the connection.
- On shutdown the input stops first, queued events finish, and then executors are released.

## Using the SDK Directly

Programs that embed the SDK instead of running the reference runtime wire it like this:

```go
client, err := mattermost.NewClient(os.Getenv("MATTERMOST_BOT_URL"), os.Getenv("MATTERMOST_BOT_TOKEN"), nil)
// ...
engine, err := pipeline.New(cfg, providers, []executor.Executor{
    mattermost.NewPostExecutor(client),
    mattermost.NewReactionExecutor(client),
})
// ...
dispatcher, err := pipeline.NewDispatcher(cfg.Dispatch, func(ctx context.Context, m ruleengine.Message) {
    _, _ = engine.Process(ctx, ruleengine.DefaultWorkflowName, m)
})
listener, err := mattermost.NewListener(client, mattermost.ListenerConfig{}, func(ctx context.Context, m ruleengine.Message) {
    _ = dispatcher.Submit(ctx, m)
})
err = listener.Start(ctx)
```

Stop the listener first, then the dispatcher, then the engine.

## Testing Bot Rules

`pkg/mattermost/mattermosttest` is an in-process fake Mattermost: it serves the REST calls the integration makes and streams events in the Mattermost 12 format. Tests can run real rules end to end without a server:

```go
srv := mattermosttest.NewServer(t)
client, _ := mattermost.NewClient(srv.URL, srv.Token, nil)
// build the engine and listener as above, then:
srv.WaitConnected(t, time.Second)
postID := srv.EmitPost(mattermosttest.PostEvent{Message: "!ping"})
replies := srv.WaitForPosts(t, 1, time.Second)   // what the bot posted
reactions := srv.WaitForReactions(t, 1, time.Second)
```

`pkg/mattermost/mattermosttest/server_test.go` is a complete example, and `internal/app/mattermost_input_test.go` runs the profile rules through the whole runtime this way.

## Production Notes

- Create the bot in **System Console → Integrations → Bot Accounts** (enable bot account creation if needed), generate a token, and add the bot to the teams and channels it should listen to. It only receives events from channels it belongs to and from its direct messages.
- Use `https` for `server_url`; the event stream then uses `wss`.
- Give the bot only the permissions it needs; a bot that only replies does not need the system admin role.
- Keep `dispatch.dedup.enabled: true` so that replayed events are not answered twice.
