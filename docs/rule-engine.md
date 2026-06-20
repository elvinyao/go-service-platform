# Rule Engine

The rule engine matches incoming messages, composes rules from providers, and produces an execution plan.

## Message

```go
type Message struct {
    ID        string
    Type      string
    Content   string
    UserID    string
    Timestamp time.Time
    Metadata  map[string]interface{}
}
```

Supported fields in conditions:

- `id`
- `type`
- `content`
- `user_id`
- `timestamp`
- `metadata.<key>`
- `metadata.<nested.key>`

## Condition Operators

`eq` matches exact normalized values:

```yaml
- field: type
  op: eq
  value: AAA
```

`contains` checks substring membership:

```yaml
- field: content
  op: contains
  value: urgent
```

`regex` uses Go regular expressions:

```yaml
- field: content
  op: regex
  value: "^deploy:"
```

## Providers

A provider supplies a snapshot of rules:

```go
type RuleProvider interface {
    Name() string
    Start(context.Context) error
    Snapshot(context.Context) RuleSet
}
```

Built-in framework provider:

- YAML provider through `ruleengine.NewYAMLProvider`

Demo provider:

- Confluence settings provider under `internal/adapters/confluence`

## Composition

The composer combines provider matches according to workflow policy:

- `single`: only the first provider is considered.
- `or`: any provider can produce rules.
- `and`: every provider must produce at least one matched rule.
- `pipeline`: every provider in `pipeline_order` must produce at least one matched rule.

## Action Merge

Actions can be deduplicated and sorted:

```yaml
action_merge:
  dedup: true
  order: priority
```

Deduplication uses `action.id` when present. If no ID is present, it uses executor type plus params.
