package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// WebhookAdapter normalizes Bitbucket Cloud events. Bitbucket signs the raw
// request body using HMAC-SHA-256 in X-Hub-Signature.
type WebhookAdapter struct{ secret string }

func NewWebhookAdapter(secret string) *WebhookAdapter { return &WebhookAdapter{secret: secret} }
func (*WebhookAdapter) Provider() scm.Provider        { return scm.ProviderBitbucket }

type pullRequestPayload struct {
	PullRequest struct {
		ID     int `json:"id"`
		Source struct {
			Commit struct {
				Hash string `json:"hash"`
			} `json:"commit"`
		} `json:"source"`
	} `json:"pullrequest"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		UUID     string `json:"uuid"`
	} `json:"repository"`
}

func (a *WebhookAdapter) Normalize(headers http.Header, body []byte, receivedAt time.Time) (string, *scm.PullRequestEvent, error) {
	if a.secret == "" {
		return "", nil, scm.ErrNotConfigured
	}
	if !scm.VerifySHA256Signature(body, headers.Get("X-Hub-Signature"), a.secret) {
		return "", nil, scm.ErrUnauthorized
	}
	deliveryID := strings.TrimSpace(headers.Get("X-Request-UUID"))
	if deliveryID == "" || len(deliveryID) > 128 {
		return "", nil, fmt.Errorf("%w: missing request UUID", scm.ErrInvalidPayload)
	}
	action, supported := bitbucketAction(headers.Get("X-Event-Key"))
	if !supported {
		return deliveryID, nil, nil
	}
	var payload pullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, scm.ErrInvalidPayload
	}
	if payload.PullRequest.ID <= 0 || payload.Repository.Name == "" || payload.Repository.FullName == "" {
		return "", nil, scm.ErrInvalidPayload
	}
	if action != scm.PullRequestClosed && payload.PullRequest.Source.Commit.Hash == "" {
		return "", nil, scm.ErrInvalidPayload
	}
	return deliveryID, &scm.PullRequestEvent{
		Provider: scm.ProviderBitbucket, DeliveryID: deliveryID, Repository: payload.Repository.FullName,
		RepositoryName: payload.Repository.Name, InstallationID: payload.Repository.UUID,
		PullRequest: payload.PullRequest.ID, HeadSHA: payload.PullRequest.Source.Commit.Hash,
		Action: action, ReceivedAt: receivedAt,
	}, nil
}

func bitbucketAction(event string) (scm.PullRequestAction, bool) {
	switch event {
	case "pullrequest:created":
		return scm.PullRequestOpened, true
	case "pullrequest:updated":
		return scm.PullRequestSynchronized, true
	case "pullrequest:fulfilled", "pullrequest:rejected":
		return scm.PullRequestClosed, true
	default:
		return "", false
	}
}
