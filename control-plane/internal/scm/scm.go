// Package scm defines the provider-neutral source-control boundary. Provider
// adapters authenticate and normalize native webhooks; the control plane only
// persists and reconciles these canonical types.
package scm

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Provider string

const (
	ProviderGitHub    Provider = "github"
	ProviderBitbucket Provider = "bitbucket"
)

type PullRequestAction string

const (
	PullRequestOpened       PullRequestAction = "opened"
	PullRequestSynchronized PullRequestAction = "synchronized"
	PullRequestReopened     PullRequestAction = "reopened"
	PullRequestClosed       PullRequestAction = "closed"
)

type CommandType string

const (
	EnsurePreviewEnvironment  CommandType = "ensure_preview_environment"
	DestroyPreviewEnvironment CommandType = "destroy_preview_environment"
)

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandLeased    CommandStatus = "leased"
	CommandSucceeded CommandStatus = "succeeded"
	CommandFailed    CommandStatus = "failed"
)

var (
	ErrUnauthorized   = errors.New("webhook authentication failed")
	ErrNotConfigured  = errors.New("webhook adapter is not configured")
	ErrInvalidPayload = errors.New("invalid webhook payload")
)

type PullRequestEvent struct {
	Provider       Provider          `json:"provider"`
	DeliveryID     string            `json:"delivery_id"`
	Repository     string            `json:"repository"`
	RepositoryName string            `json:"repository_name"`
	InstallationID string            `json:"installation_id,omitempty"`
	PullRequest    int               `json:"pull_request"`
	HeadSHA        string            `json:"head_sha,omitempty"`
	Action         PullRequestAction `json:"action"`
	ReceivedAt     time.Time         `json:"received_at"`
}

type LifecycleCommand struct {
	ID             string        `json:"id"`
	Provider       Provider      `json:"provider"`
	DeliveryID     string        `json:"delivery_id"`
	Type           CommandType   `json:"type"`
	Repository     string        `json:"repository"`
	InstallationID string        `json:"installation_id,omitempty"`
	PullRequest    int           `json:"pull_request"`
	HeadSHA        string        `json:"head_sha,omitempty"`
	Environment    string        `json:"environment"`
	Status         CommandStatus `json:"status"`
	Attempts       int           `json:"attempts"`
	AvailableAt    time.Time     `json:"available_at"`
	LeaseUntil     *time.Time    `json:"lease_until,omitempty"`
	LastError      string        `json:"last_error,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

type WebhookAdapter interface {
	Provider() Provider
	Normalize(headers http.Header, body []byte, receivedAt time.Time) (deliveryID string, event *PullRequestEvent, err error)
}

type InstallationToken struct {
	Value     string
	ExpiresAt time.Time
}

// Authenticator deliberately abstracts provider-specific credentials. Tokens
// remain ephemeral and must never be serialized into LifecycleCommand.
type Authenticator interface {
	Provider() Provider
	Token(ctx context.Context, installationID string) (InstallationToken, error)
}

var nonDNSLabelCharacters = regexp.MustCompile(`[^a-z0-9-]+`)

func CommandFromEvent(event PullRequestEvent) *LifecycleCommand {
	var commandType CommandType
	switch event.Action {
	case PullRequestOpened, PullRequestSynchronized, PullRequestReopened:
		commandType = EnsurePreviewEnvironment
	case PullRequestClosed:
		commandType = DestroyPreviewEnvironment
	default:
		return nil
	}
	return &LifecycleCommand{
		ID:       string(event.Provider) + ":" + event.DeliveryID,
		Provider: event.Provider, DeliveryID: event.DeliveryID, Type: commandType,
		Repository: event.Repository, InstallationID: event.InstallationID,
		PullRequest: event.PullRequest, HeadSHA: event.HeadSHA,
		Environment: PreviewEnvironmentName(event.RepositoryName, event.PullRequest),
		Status:      CommandPending, AvailableAt: event.ReceivedAt, CreatedAt: event.ReceivedAt,
	}
}

func DeliveryKey(provider Provider, deliveryID string) string {
	return string(provider) + ":" + deliveryID
}

func PreviewEnvironmentName(repository string, pullRequest int) string {
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
