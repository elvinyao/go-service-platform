package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/executor"
	"github.com/elvinyao/go-service-platform/pkg/pipeline"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

const maxEventBodyBytes = 1 << 20

type eventResponse struct {
	MessageID      string   `json:"message_id"`
	Workflow       string   `json:"workflow"`
	MatchedRules   []string `json:"matched_rules"`
	PlannedActions []string `json:"planned_actions"`
	Error          string   `json:"error,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	provider := ruleengine.NewYAMLProvider(
		"yaml",
		getenv("WORKFLOW_RULES", "config/workflow-rules.yaml"),
		ruleengine.DefaultWorkflowName,
	)
	engine, err := pipeline.New(
		ruleengine.DefaultEngineConfig(),
		[]ruleengine.RuleProvider{provider},
		[]executor.Executor{executor.NewLogExecutor(), executor.NewHTTPExecutor()},
	)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}
	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("start pipeline: %w", err)
	}

	server := &http.Server{
		Addr:              getenv("EVENT_GATEWAY_ADDR", "127.0.0.1:18081"),
		Handler:           newHandler(engine),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("event gateway listening on http://%s", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve event gateway: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down event gateway: %w", err)
	}
	return nil
}

func newHandler(engine *pipeline.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /pipeline", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, engine.Snapshot(r.Context()))
	})
	mux.HandleFunc("POST /events", func(w http.ResponseWriter, r *http.Request) {
		handleEvent(w, r, engine)
	})
	return mux
}

func handleEvent(w http.ResponseWriter, r *http.Request, engine *pipeline.Engine) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEventBodyBytes))
	decoder.DisallowUnknownFields()

	var message ruleengine.Message
	if err := decoder.Decode(&message); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "decode event: " + err.Error()})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if message.ID == "" || message.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and type are required"})
		return
	}
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now().UTC()
	}

	plan, err := engine.Process(r.Context(), ruleengine.DefaultWorkflowName, message)
	response := eventResponse{
		MessageID:      message.ID,
		Workflow:       plan.Workflow,
		MatchedRules:   ruleIDs(plan.Rules),
		PlannedActions: actionIDs(plan.Actions),
	}
	if err != nil {
		response.Error = err.Error()
		status := http.StatusInternalServerError
		if pipeline.IsExecutionError(err) {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, response)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing data: %w", err)
	}
	return fmt.Errorf("request body must contain exactly one JSON object")
}

func ruleIDs(rules []ruleengine.Rule) []string {
	ids := make([]string, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	return ids
}

func actionIDs(actions []ruleengine.Action) []string {
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ID)
	}
	return ids
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
