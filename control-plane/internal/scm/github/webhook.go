package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

type WebhookAdapter struct{ secret string }

func NewWebhookAdapter(secret string) *WebhookAdapter { return &WebhookAdapter{secret: secret} }
func (*WebhookAdapter) Provider() scm.Provider        { return scm.ProviderGitHub }

type pullRequestPayload struct {
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

func (a *WebhookAdapter) Normalize(headers http.Header, body []byte, receivedAt time.Time) (string, *scm.PullRequestEvent, error) {
	if a.secret == "" {
		return "", nil, scm.ErrNotConfigured
	}
	if !scm.VerifySHA256Signature(body, headers.Get("X-Hub-Signature-256"), a.secret) {
		return "", nil, scm.ErrUnauthorized
	}
	deliveryID := strings.TrimSpace(headers.Get("X-GitHub-Delivery"))
	if deliveryID == "" || len(deliveryID) > 128 {
		return "", nil, fmt.Errorf("%w: missing delivery ID", scm.ErrInvalidPayload)
	}
	if headers.Get("X-GitHub-Event") != "pull_request" {
		return deliveryID, nil, nil
	}
	var payload pullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, scm.ErrInvalidPayload
	}
	action, supported := githubAction(payload.Action)
	if !supported {
		return deliveryID, nil, nil
	}
	if payload.Number <= 0 || payload.Repository.Name == "" || payload.Repository.FullName == "" || payload.Installation.ID <= 0 {
		return "", nil, scm.ErrInvalidPayload
	}
	if action != scm.PullRequestClosed && payload.PullRequest.Head.SHA == "" {
		return "", nil, scm.ErrInvalidPayload
	}
	return deliveryID, &scm.PullRequestEvent{
		Provider: scm.ProviderGitHub, DeliveryID: deliveryID, Repository: payload.Repository.FullName,
		RepositoryName: payload.Repository.Name, InstallationID: strconv.FormatInt(payload.Installation.ID, 10),
		PullRequest: payload.Number, HeadSHA: payload.PullRequest.Head.SHA, Action: action, ReceivedAt: receivedAt,
	}, nil
}

func githubAction(action string) (scm.PullRequestAction, bool) {
	switch action {
	case "opened":
		return scm.PullRequestOpened, true
	case "synchronize":
		return scm.PullRequestSynchronized, true
	case "reopened":
		return scm.PullRequestReopened, true
	case "closed":
		return scm.PullRequestClosed, true
	default:
		return "", false
	}
}
