package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const maxGitHubWebhookBody = 1 << 20

var nonDNSLabelCharacters = regexp.MustCompile(`[^a-z0-9-]+`)

type GitHubWebhookCommand struct {
	DeliveryID     string    `json:"delivery_id"`
	Type           string    `json:"type"`
	Repository     string    `json:"repository"`
	InstallationID int64     `json:"installation_id"`
	PullRequest    int       `json:"pull_request"`
	HeadSHA        string    `json:"head_sha"`
	Environment    string    `json:"environment"`
	ReceivedAt     time.Time `json:"received_at"`
}

type pullRequestWebhook struct {
	Action       string `json:"action"`
	Number       int    `json:"number"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

func (h *Handlers) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if h.githubWebhookSecret == "" {
		http.Error(w, "GitHub webhook ingestion is not configured", http.StatusServiceUnavailable)
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	if deliveryID == "" || len(deliveryID) > 128 {
		http.Error(w, "missing or invalid GitHub delivery ID", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGitHubWebhookBody))
	if err != nil {
		http.Error(w, "invalid webhook payload", http.StatusRequestEntityTooLarge)
		return
	}
	if !validGitHubSignature(body, r.Header.Get("X-Hub-Signature-256"), h.githubWebhookSecret) {
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}

	receivedAt := time.Now().UTC()
	var command *GitHubWebhookCommand
	event := r.Header.Get("X-GitHub-Event")
	if event == "pull_request" {
		parsed, parseErr := parsePullRequestCommand(body, deliveryID, receivedAt)
		if parseErr != nil {
			http.Error(w, parseErr.Error(), http.StatusBadRequest)
			return
		}
		command = parsed
	}

	duplicate, err := h.store.RecordGitHubDelivery(deliveryID, command, receivedAt)
	if err != nil {
		h.logger.Error("failed to persist GitHub delivery", zap.String("delivery_id", deliveryID), zap.Error(err))
		http.Error(w, "failed to persist webhook delivery", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"accepted":        true,
		"duplicate":       duplicate,
		"command_created": command != nil && !duplicate,
	})
}

func validGitHubSignature(body []byte, signature, secret string) bool {
	const prefix = "sha256="
	if len(signature) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(signature, prefix) {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func parsePullRequestCommand(body []byte, deliveryID string, receivedAt time.Time) (*GitHubWebhookCommand, error) {
	var payload pullRequestWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid pull request payload")
	}
	commandType := ""
	switch payload.Action {
	case "opened", "synchronize", "reopened":
		commandType = "upsert_preview_environment"
	case "closed":
		commandType = "destroy_preview_environment"
	default:
		return nil, nil
	}
	if payload.Number <= 0 || payload.Repository.Name == "" || payload.Repository.FullName == "" || payload.Installation.ID <= 0 {
		return nil, fmt.Errorf("pull request payload is missing required identity fields")
	}
	if commandType == "upsert_preview_environment" && payload.PullRequest.Head.SHA == "" {
		return nil, fmt.Errorf("pull request payload is missing head SHA")
	}
	return &GitHubWebhookCommand{
		DeliveryID:     deliveryID,
		Type:           commandType,
		Repository:     payload.Repository.FullName,
		InstallationID: payload.Installation.ID,
		PullRequest:    payload.Number,
		HeadSHA:        payload.PullRequest.Head.SHA,
		Environment:    previewEnvironmentName(payload.Repository.Name, payload.Number),
		ReceivedAt:     receivedAt,
	}, nil
}

func previewEnvironmentName(repository string, pullRequest int) string {
	base := strings.Trim(nonDNSLabelCharacters.ReplaceAllString(strings.ToLower(repository), "-"), "-")
	suffix := "-pr-" + strconv.Itoa(pullRequest)
	maxBase := 63 - len(suffix)
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	if base == "" {
		base = "preview"
	}
	return base + suffix
}
