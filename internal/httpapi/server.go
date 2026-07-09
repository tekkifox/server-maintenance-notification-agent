package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"server-maintenance-notification-agent/internal/service"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type DiscordWebhookPayload struct {
	Content string `json:"content"`
}

type Server struct {
	notifier        *service.Notifier
	dockerCommander *service.DockerCommander
}

func NewServer(notifier *service.Notifier, dockerCommander *service.DockerCommander) *Server {
	return &Server{notifier: notifier, dockerCommander: dockerCommander}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /v1/discord/broadcast", s.discordBroadcast)
	mux.HandleFunc("POST /v1/console/command", s.dockerCommand)
	mux.HandleFunc("POST /v1/message/broadcast", s.dockerBroadcast)
	mux.HandleFunc("POST /v1/console/broadcast", s.dockerBroadcast)
	mux.HandleFunc("POST /v1/webhooks/discord", s.discordWebhook)
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
	return requestLogger(mux)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("received request: %s %s from %s body_error=%v", r.Method, r.URL.Path, r.RemoteAddr, err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unable to read request body"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		log.Printf("received request: %s %s from %s%s", r.Method, r.URL.Path, r.RemoteAddr, describeRequestParameters(r, body))
		next.ServeHTTP(rw, r)
		log.Printf("completed request: %s %s status=%d duration=%s", r.Method, r.URL.Path, rw.status, time.Since(start).Round(time.Millisecond))
	})
}

func describeRequestParameters(r *http.Request, body []byte) string {
	parts := make([]string, 0, 2)
	if query := strings.TrimSpace(r.URL.RawQuery); query != "" {
		parts = append(parts, "query="+query)
	}
	if len(body) > 0 {
		parts = append(parts, "body="+summarizeRequestBody(r.Header.Get("Content-Type"), body))
	}
	if len(parts) == 0 {
		return ""
	}
	return " params{" + strings.Join(parts, " ") + "}"
}

func summarizeRequestBody(contentType string, body []byte) string {
	const maxLoggedBodyBytes = 4096

	trimmed := body
	truncated := false
	if len(trimmed) > maxLoggedBodyBytes {
		trimmed = trimmed[:maxLoggedBodyBytes]
		truncated = true
	}

	if strings.Contains(strings.ToLower(contentType), "json") {
		if redacted, err := redactJSONBody(trimmed); err == nil {
			if truncated {
				return redacted + "..."
			}
			return redacted
		}
	}

	text := string(trimmed)
	if truncated {
		return text + "..."
	}
	return text
}

func redactJSONBody(body []byte) (string, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}

	redactJSONValue(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, redacted); err != nil {
		return string(redacted), nil
	}
	return compact.String(), nil
}

func redactJSONValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if isSensitiveJSONKey(key) {
				v[key] = "[redacted]"
				continue
			}
			redactJSONValue(child)
		}
	case []any:
		for _, child := range v {
			redactJSONValue(child)
		}
	}
}

func isSensitiveJSONKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "password", "token", "secret", "rcon_password":
		return true
	default:
		return false
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// healthz godoc
// @Summary Health check
// @Description Returns service health status.
// @Tags system
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /healthz [get]
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// discordBroadcast godoc
// @Summary Send a Discord broadcast
// @Description Sends a message to one or more Discord channels.
// @Tags discord
// @Accept json
// @Produce json
// @Param dry_run query bool false "Dry run request"
// @Param request body service.TriggerRequest true "Discord broadcast request"
// @Success 202 {object} service.TriggerResult
// @Failure 400 {object} ErrorResponse
// @Failure 408 {object} ErrorResponse
// @Router /v1/discord/broadcast [post]
func (s *Server) discordBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req service.TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
		return
	}
	if dryRun, ok := parseBoolQuery(r, "dry_run"); ok {
		req.DryRun = dryRun
	}

	result, err := s.notifier.Trigger(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

// discordWebhook godoc
// @Summary Receive a Discord webhook message
// @Description Receives a Discord-compatible webhook message and forwards/broadcasts it to Discord channels.
// @Tags webhooks
// @Accept json
// @Produce json
// @Param channel_id query string false "Optional Discord channel ID"
// @Param channel_ids query string false "Optional comma-separated Discord channel IDs"
// @Param dry_run query bool false "Dry run request"
// @Param request body DiscordWebhookPayload true "Discord Webhook payload"
// @Success 202 {object} service.TriggerResult
// @Failure 400 {object} ErrorResponse
// @Failure 408 {object} ErrorResponse
// @Router /v1/webhooks/discord [post]
func (s *Server) discordWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var payload DiscordWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
		return
	}

	var channelIDs []string
	if cid := r.URL.Query().Get("channel_id"); cid != "" {
		channelIDs = append(channelIDs, cid)
	}
	if cids := r.URL.Query().Get("channel_ids"); cids != "" {
		for _, id := range strings.Split(cids, ",") {
			channelIDs = append(channelIDs, strings.TrimSpace(id))
		}
	}

	req := service.TriggerRequest{
		Message:    payload.Content,
		ChannelIDs: channelIDs,
	}

	if dryRun, ok := parseBoolQuery(r, "dry_run"); ok {
		req.DryRun = dryRun
	}

	result, err := s.notifier.Trigger(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

// dockerCommand godoc
// @Summary Send a raw console command
// @Description Sends the provided command directly to one or more containers without templating.
// @Tags console
// @Accept json
// @Produce json
// @Param dry_run query bool false "Dry run request"
// @Param request body service.DockerCommandRequest true "Raw command request"
// @Success 202 {object} service.DockerCommandResult
// @Failure 400 {object} ErrorResponse
// @Failure 408 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/console/command [post]
func (s *Server) dockerCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if s.dockerCommander == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "docker control is not configured"})
		return
	}

	var req service.DockerCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
		return
	}

	if req.ContainerID == "" {
		req.ContainerID = r.PathValue("container_id")
	}
	if dryRun, ok := parseBoolQuery(r, "dry_run"); ok {
		req.DryRun = dryRun
	}

	result, err := s.dockerCommander.Send(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}
	logJSON("console command response", result)

	writeJSON(w, http.StatusAccepted, result)
}

// dockerBroadcast godoc
// @Summary Broadcast a message command to consoles
// @Description Broadcasts a game-specific message command to one or more consoles, optionally in dry-run mode.
// @Tags console
// @Accept json
// @Produce json
// @Param dry_run query bool false "Dry run request"
// @Param request body service.BroadcastRequest true "Broadcast request"
// @Success 202 {object} service.BroadcastResult
// @Failure 400 {object} ErrorResponse
// @Failure 408 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /v1/message/broadcast [post]
func (s *Server) dockerBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if s.dockerCommander == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "docker control is not configured"})
		return
	}

	var req service.BroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
		return
	}

	if req.ContainerID == "" {
		req.ContainerID = r.PathValue("container_id")
	}
	if dryRun, ok := parseBoolQuery(r, "dry_run"); ok {
		req.DryRun = dryRun
	}

	result, err := s.dockerCommander.Broadcast(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func parseBoolQuery(r *http.Request, key string) (bool, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return false, false
	}
	switch strings.ToLower(value) {
	case "1", "true", "t", "yes", "y", "on":
		return true, true
	case "0", "false", "f", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func logJSON(prefix string, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		log.Printf("%s: marshal_error=%v", prefix, err)
		return
	}
	log.Printf("%s: %s", prefix, payload)
}
