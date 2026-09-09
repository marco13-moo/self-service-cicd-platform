package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

type StatusReporter struct {
	auth    scm.Authenticator
	client  *http.Client
	baseURL string
}

func NewStatusReporter(auth scm.Authenticator, client *http.Client) *StatusReporter {
	if client == nil {
		client = http.DefaultClient
	}
	return &StatusReporter{auth: auth, client: client, baseURL: "https://api.github.com"}
}
func (r *StatusReporter) Provider() scm.Provider { return scm.ProviderGitHub }
func (r *StatusReporter) Report(ctx context.Context, status scm.RevisionStatus) error {
	if status.Repository == "" || status.SHA == "" {
		return fmt.Errorf("repository and SHA are required")
	}
	token, err := r.auth.Token(ctx, status.InstallationID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"state": string(status.State), "context": "self-service-cicd/preview", "description": status.Description, "target_url": status.TargetURL})
	endpoint := strings.TrimRight(r.baseURL, "/") + "/repos/" + strings.Trim(status.Repository, "/") + "/statuses/" + status.SHA
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.Value)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GitHub status API returned %s", response.Status)
	}
	return nil
}
