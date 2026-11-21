// Command provision prepares a local Mattermost for bot development.
//
// It talks only to the REST API, so it works against any Mattermost you can
// reach, and it is safe to run more than once. It creates (or reuses) an admin
// user, a team, a test channel, and a bot account that is a member of both,
// then writes the bot's access token and IDs to an env file:
//
//	go run ./deploy/mattermost/provision
//
// The first account created on a fresh Mattermost becomes its system admin,
// which is how the admin is bootstrapped without any other credentials.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type settings struct {
	url           string
	adminUser     string
	adminPassword string
	adminEmail    string
	team          string
	channel       string
	bot           string
	envFile       string
	wait          time.Duration
}

func main() {
	var s settings
	flag.StringVar(&s.url, "url", envOr("MATTERMOST_BOT_URL", "http://localhost:8065"), "Mattermost base URL")
	flag.StringVar(&s.adminUser, "admin-user", "admin", "admin username")
	flag.StringVar(&s.adminPassword, "admin-password", "Admin-Passw0rd!", "admin password (local use only)")
	flag.StringVar(&s.adminEmail, "admin-email", "admin@example.com", "admin email")
	flag.StringVar(&s.team, "team", "dev", "team name")
	flag.StringVar(&s.channel, "channel", "bot-test", "test channel name")
	flag.StringVar(&s.bot, "bot", "gsp-bot", "bot username")
	flag.StringVar(&s.envFile, "env-file", "deploy/mattermost/.env", "where to write the bot settings")
	flag.DurationVar(&s.wait, "wait", 3*time.Minute, "how long to wait for Mattermost to come up")
	flag.Parse()

	if err := run(context.Background(), s); err != nil {
		fmt.Fprintln(os.Stderr, "provision:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, s settings) error {
	api := &apiClient{base: strings.TrimRight(s.url, "/"), http: &http.Client{Timeout: 15 * time.Second}}

	fmt.Printf("Waiting for Mattermost at %s ...\n", api.base)
	if err := api.waitReady(ctx, s.wait); err != nil {
		return err
	}

	adminID, err := ensureAdmin(ctx, api, s)
	if err != nil {
		return err
	}
	team, err := ensureTeam(ctx, api, s.team)
	if err != nil {
		return err
	}
	channel, err := ensureChannel(ctx, api, team, s.channel)
	if err != nil {
		return err
	}
	botID, err := ensureBot(ctx, api, s.bot)
	if err != nil {
		return err
	}
	for _, userID := range []string{adminID, botID} {
		if err := api.call(ctx, http.MethodPost, "/teams/"+team+"/members", map[string]string{"team_id": team, "user_id": userID}, nil); err != nil {
			return fmt.Errorf("add user to team: %w", err)
		}
		if err := api.call(ctx, http.MethodPost, "/channels/"+channel+"/members", map[string]string{"user_id": userID}, nil); err != nil {
			return fmt.Errorf("add user to channel: %w", err)
		}
	}

	token, reused, err := ensureBotToken(ctx, api, botID, s.envFile)
	if err != nil {
		return err
	}
	if err := writeEnvFile(s.envFile, map[string]string{
		"MATTERMOST_BOT_URL":         api.base,
		"MATTERMOST_BOT_TOKEN":       token,
		"MATTERMOST_BOT_USER_ID":     botID,
		"MATTERMOST_TEAM_ID":         team,
		"MATTERMOST_TEST_CHANNEL_ID": channel,
	}); err != nil {
		return err
	}

	tokenNote := "new token"
	if reused {
		tokenNote = "existing token still valid"
	}
	fmt.Printf(`
Mattermost is ready.
  Web UI:        %s/%s/channels/%s
  Admin login:   %s / %s
  Bot:           @%s (%s)
  Bot settings:  %s

Start the bot with: make mattermost-bot
`, api.base, s.team, s.channel, s.adminUser, s.adminPassword, s.bot, tokenNote, s.envFile)
	return nil
}

func ensureAdmin(ctx context.Context, api *apiClient, s settings) (string, error) {
	token, userID, err := api.login(ctx, s.adminUser, s.adminPassword)
	if err != nil {
		var apiErr *apiError
		if !errors.As(err, &apiErr) || apiErr.status != http.StatusUnauthorized {
			return "", fmt.Errorf("log in as %s: %w", s.adminUser, err)
		}
		// No such user yet: on a fresh server the first account becomes admin.
		var created struct {
			ID    string `json:"id"`
			Roles string `json:"roles"`
		}
		if err := api.call(ctx, http.MethodPost, "/users", map[string]string{
			"username": s.adminUser, "password": s.adminPassword, "email": s.adminEmail,
		}, &created); err != nil {
			return "", fmt.Errorf("create admin %s: %w", s.adminUser, err)
		}
		if token, userID, err = api.login(ctx, s.adminUser, s.adminPassword); err != nil {
			return "", fmt.Errorf("log in as new admin: %w", err)
		}
	}
	api.token = token

	var me struct {
		Roles string `json:"roles"`
	}
	if err := api.call(ctx, http.MethodGet, "/users/me", nil, &me); err != nil {
		return "", err
	}
	if !strings.Contains(me.Roles, "system_admin") {
		return "", fmt.Errorf("user %s exists but is not a system admin; use a fresh Mattermost or pass an admin with -admin-user", s.adminUser)
	}
	return userID, nil
}

func ensureTeam(ctx context.Context, api *apiClient, name string) (string, error) {
	var team struct {
		ID string `json:"id"`
	}
	err := api.call(ctx, http.MethodGet, "/teams/name/"+name, nil, &team)
	if isNotFound(err) {
		err = api.call(ctx, http.MethodPost, "/teams", map[string]string{
			"name": name, "display_name": strings.ToUpper(name[:1]) + name[1:], "type": "O",
		}, &team)
	}
	if err != nil {
		return "", fmt.Errorf("team %s: %w", name, err)
	}
	return team.ID, nil
}

func ensureChannel(ctx context.Context, api *apiClient, teamID, name string) (string, error) {
	var channel struct {
		ID string `json:"id"`
	}
	err := api.call(ctx, http.MethodGet, "/teams/"+teamID+"/channels/name/"+name, nil, &channel)
	if isNotFound(err) {
		err = api.call(ctx, http.MethodPost, "/channels", map[string]string{
			"team_id": teamID, "name": name, "display_name": "Bot Test", "type": "O",
		}, &channel)
	}
	if err != nil {
		return "", fmt.Errorf("channel %s: %w", name, err)
	}
	return channel.ID, nil
}

func ensureBot(ctx context.Context, api *apiClient, username string) (string, error) {
	var user struct {
		ID    string `json:"id"`
		IsBot bool   `json:"is_bot"`
	}
	err := api.call(ctx, http.MethodGet, "/users/username/"+username, nil, &user)
	if err == nil {
		if !user.IsBot {
			return "", fmt.Errorf("user %s exists but is not a bot", username)
		}
		return user.ID, nil
	}
	if !isNotFound(err) {
		return "", fmt.Errorf("look up bot %s: %w", username, err)
	}
	var bot struct {
		UserID string `json:"user_id"`
	}
	if err := api.call(ctx, http.MethodPost, "/bots", map[string]string{
		"username": username, "display_name": "GSP Bot", "description": "go-service-platform local test bot",
	}, &bot); err != nil {
		return "", fmt.Errorf("create bot %s: %w", username, err)
	}
	return bot.UserID, nil
}

// ensureBotToken reuses the token in envFile when it still authenticates as
// the bot, so repeated runs do not pile up tokens.
func ensureBotToken(ctx context.Context, api *apiClient, botID, envFile string) (string, bool, error) {
	if existing := readEnvFile(envFile)["MATTERMOST_BOT_TOKEN"]; existing != "" {
		check := &apiClient{base: api.base, http: api.http, token: existing}
		var me struct {
			ID string `json:"id"`
		}
		if err := check.call(ctx, http.MethodGet, "/users/me", nil, &me); err == nil && me.ID == botID {
			return existing, true, nil
		}
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := api.call(ctx, http.MethodPost, "/users/"+botID+"/tokens", map[string]string{"description": "local development"}, &token); err != nil {
		return "", false, fmt.Errorf("create bot token: %w", err)
	}
	return token.Token, false, nil
}

func readEnvFile(path string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(key, "#") {
			values[key] = value
		}
	}
	return values
}

func writeEnvFile(path string, values map[string]string) error {
	var out strings.Builder
	out.WriteString("# Written by deploy/mattermost/provision. Local development only; do not commit.\n")
	for _, key := range []string{"MATTERMOST_BOT_URL", "MATTERMOST_BOT_TOKEN", "MATTERMOST_BOT_USER_ID", "MATTERMOST_TEAM_ID", "MATTERMOST_TEST_CHANNEL_ID"} {
		fmt.Fprintf(&out, "%s=%s\n", key, values[key])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out.String()), 0o600)
}

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("status %d: %s", e.status, e.message) }

func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.status == http.StatusNotFound
}

func (c *apiClient) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := c.call(ctx, http.MethodGet, "/system/ping", nil, nil); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return fmt.Errorf("Mattermost at %s did not become ready within %v: %w", c.base, timeout, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func (c *apiClient) login(ctx context.Context, username, password string) (string, string, error) {
	var user struct {
		ID string `json:"id"`
	}
	header, err := c.request(ctx, http.MethodPost, "/users/login", map[string]string{"login_id": username, "password": password}, &user)
	if err != nil {
		return "", "", err
	}
	return header.Get("Token"), user.ID, nil
}

func (c *apiClient) call(ctx context.Context, method, path string, body, out interface{}) error {
	_, err := c.request(ctx, method, path, body, out)
	return err
}

func (c *apiClient) request(ctx context.Context, method, path string, body, out interface{}) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v4"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var mmErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &mmErr)
		if mmErr.Message == "" {
			mmErr.Message = strings.TrimSpace(string(data))
		}
		return nil, &apiError{status: resp.StatusCode, message: mmErr.Message}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return resp.Header, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
