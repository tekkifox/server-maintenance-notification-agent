package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxWebhookBodyBytes = 1 << 20

func readLimitedBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}

	return body, nil
}

func channelIDsFromQuery(r *http.Request) []string {
	query := r.URL.Query()
	result := make([]string, 0)
	seen := make(map[string]struct{})

	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			channelID := strings.TrimSpace(part)
			if channelID == "" {
				continue
			}
			if _, ok := seen[channelID]; ok {
				continue
			}
			seen[channelID] = struct{}{}
			result = append(result, channelID)
		}
	}

	for _, value := range query["channel_id"] {
		add(value)
	}
	for _, value := range query["channel_ids"] {
		add(value)
	}

	return result
}

type githubDeployPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Deployment struct {
		Environment string `json:"environment"`
		Description string `json:"description"`
		Task        string `json:"task"`
		Creator     struct {
			Login string `json:"login"`
		} `json:"creator"`
	} `json:"deployment"`
	DeploymentStatus struct {
		State          string `json:"state"`
		Description    string `json:"description"`
		Environment    string `json:"environment"`
		EnvironmentURL string `json:"environment_url"`
		TargetURL      string `json:"target_url"`
	} `json:"deployment_status"`
	WorkflowRun struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"head_branch"`
		HTMLURL    string `json:"html_url"`
	} `json:"workflow_run"`
}

func formatGitHubDeployMessage(event string, body []byte) (string, error) {
	var payload githubDeployPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode github payload: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(event)) {
	case "deployment_status":
		return formatGitHubDeploymentStatus(payload), nil
	case "deployment":
		return formatGitHubDeployment(payload), nil
	case "workflow_run":
		return formatGitHubWorkflowRun(payload), nil
	default:
		if payload.DeploymentStatus.State != "" {
			return formatGitHubDeploymentStatus(payload), nil
		}
		if payload.WorkflowRun.Name != "" {
			return formatGitHubWorkflowRun(payload), nil
		}
		return formatGitHubDeployment(payload), nil
	}
}

func formatGitHubDeploymentStatus(payload githubDeployPayload) string {
	repo := firstNonEmpty(payload.Repository.FullName, payload.Repository.Name, "unknown repository")
	environment := firstNonEmpty(payload.DeploymentStatus.Environment, payload.Deployment.Environment)
	state := firstNonEmpty(payload.DeploymentStatus.State, "unknown")
	actor := firstNonEmpty(payload.Sender.Login, payload.Deployment.Creator.Login)
	description := firstNonEmpty(payload.DeploymentStatus.Description, payload.Deployment.Description)
	targetURL := firstNonEmpty(payload.DeploymentStatus.EnvironmentURL, payload.DeploymentStatus.TargetURL, payload.Repository.HTMLURL)

	lines := []string{fmt.Sprintf("%s GitHub deployment status", githubStateEmoji(state))}
	parts := []string{repo}
	if environment != "" {
		parts = append(parts, "env="+environment)
	}
	if state != "" {
		parts = append(parts, "state="+state)
	}
	if actor != "" {
		parts = append(parts, "actor="+actor)
	}
	lines = append(lines, strings.Join(parts, " | "))
	if description != "" {
		lines = append(lines, description)
	}
	if targetURL != "" {
		lines = append(lines, targetURL)
	}

	return strings.Join(lines, "\n")
}

func formatGitHubDeployment(payload githubDeployPayload) string {
	repo := firstNonEmpty(payload.Repository.FullName, payload.Repository.Name, "unknown repository")
	environment := firstNonEmpty(payload.Deployment.Environment, "unknown environment")
	actor := firstNonEmpty(payload.Sender.Login, payload.Deployment.Creator.Login)
	description := firstNonEmpty(payload.Deployment.Description, payload.Deployment.Task)

	lines := []string{"GitHub deployment"}
	parts := []string{repo, "env=" + environment}
	if actor != "" {
		parts = append(parts, "actor="+actor)
	}
	lines = append(lines, strings.Join(parts, " | "))
	if description != "" {
		lines = append(lines, description)
	}

	return strings.Join(lines, "\n")
}

func formatGitHubWorkflowRun(payload githubDeployPayload) string {
	repo := firstNonEmpty(payload.Repository.FullName, payload.Repository.Name, "unknown repository")
	workflow := firstNonEmpty(payload.WorkflowRun.Name, "workflow")
	branch := firstNonEmpty(payload.WorkflowRun.HeadBranch, "unknown branch")
	status := firstNonEmpty(payload.WorkflowRun.Conclusion, payload.WorkflowRun.Status, "unknown")
	actor := firstNonEmpty(payload.Sender.Login, "unknown actor")
	url := firstNonEmpty(payload.WorkflowRun.HTMLURL, payload.Repository.HTMLURL)

	lines := []string{fmt.Sprintf("%s GitHub workflow run", githubStateEmoji(status))}
	parts := []string{repo, "workflow=" + workflow, "branch=" + branch, "status=" + status, "actor=" + actor}
	lines = append(lines, strings.Join(parts, " | "))
	if url != "" {
		lines = append(lines, url)
	}

	return strings.Join(lines, "\n")
}

func githubStateEmoji(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success", "completed":
		return "[OK]"
	case "failure", "error", "cancelled", "timed_out":
		return "[FAIL]"
	case "pending", "in_progress", "queued", "requested":
		return "[WAIT]"
	default:
		return "[INFO]"
	}
}

type portainerWebhookPayload struct {
	Event         string `json:"event"`
	Action        string `json:"action"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	State         string `json:"state"`
	Result        string `json:"result"`
	StackName     string `json:"stack_name"`
	Stack         string `json:"stack"`
	ServiceName   string `json:"service_name"`
	Service       string `json:"service"`
	ContainerName string `json:"container_name"`
	Container     string `json:"container"`
	Image         string `json:"image"`
	Tag           string `json:"tag"`
	EndpointName  string `json:"endpoint_name"`
	Endpoint      string `json:"endpoint"`
	Details       string `json:"details"`
	Message       string `json:"message"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Actor         string `json:"actor"`
}

func formatPortainerWebhookMessage(body []byte) (string, error) {
	var payload portainerWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode portainer payload: %w", err)
	}

	return formatPortainerMessage(payload), nil
}

func formatPortainerMessage(payload portainerWebhookPayload) string {
	label := firstNonEmpty(payload.Event, payload.Action, payload.Type, "webhook")
	target := firstNonEmpty(payload.StackName, payload.Stack, payload.ServiceName, payload.Service, payload.ContainerName, payload.Container, payload.Name)
	status := firstNonEmpty(payload.Status, payload.State, payload.Result)
	endpoint := firstNonEmpty(payload.EndpointName, payload.Endpoint)
	image := firstNonEmpty(payload.Image, payload.Tag)
	actor := strings.TrimSpace(payload.Actor)
	details := firstNonEmpty(payload.Details, payload.Message)
	url := strings.TrimSpace(payload.URL)

	lines := []string{fmt.Sprintf("Portainer webhook: %s", label)}
	parts := make([]string, 0, 6)
	if target != "" {
		parts = append(parts, target)
	}
	if endpoint != "" {
		parts = append(parts, "endpoint="+endpoint)
	}
	if image != "" {
		parts = append(parts, "image="+image)
	}
	if status != "" {
		parts = append(parts, "status="+status)
	}
	if actor != "" {
		parts = append(parts, "actor="+actor)
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, " | "))
	}
	if details != "" {
		lines = append(lines, details)
	}
	if url != "" {
		lines = append(lines, url)
	}

	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
