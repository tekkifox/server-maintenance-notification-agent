package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"server-maintenance-notification-agent/internal/service"
)

type Server struct {
	notifier *service.Notifier
}

func NewServer(notifier *service.Notifier) *Server {
	return &Server{notifier: notifier}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /v1/trigger", s.trigger)
	mux.HandleFunc("POST /v1/webhooks/github/deploy", s.githubDeploy)
	mux.HandleFunc("POST /v1/webhooks/portainer", s.portainerWebhook)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) trigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req service.TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := s.notifier.Trigger(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) githubDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	body, err := readLimitedBody(w, r, maxWebhookBodyBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	message, err := formatGitHubDeployMessage(r.Header.Get("X-GitHub-Event"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.dispatchWebhookMessage(w, r, message)
}

func (s *Server) portainerWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	body, err := readLimitedBody(w, r, maxWebhookBodyBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	message, err := formatPortainerWebhookMessage(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.dispatchWebhookMessage(w, r, message)
}

func (s *Server) dispatchWebhookMessage(w http.ResponseWriter, r *http.Request, message string) {
	result, err := s.notifier.Trigger(r.Context(), service.TriggerRequest{
		Message:    message,
		ChannelIDs: channelIDsFromQuery(r),
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
