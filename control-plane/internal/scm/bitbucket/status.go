package bitbucket

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
	return &StatusReporter{auth: auth, client: client, baseURL: "https://api.bitbucket.org/2.0"}
}
func (r *StatusReporter) Provider() scm.Provider { return scm.ProviderBitbucket }
func (r *StatusReporter) Report(ctx context.Context, status scm.RevisionStatus) error {
	if status.Repository == "" || status.SHA == "" {
		return fmt.Errorf("repository and SHA are required")
	}
	native := map[scm.RevisionState]string{scm.RevisionPending: "INPROGRESS", scm.RevisionSuccess: "SUCCESSFUL", scm.RevisionFailure: "FAILED"}[status.State]
	if native == "" {
		return fmt.Errorf("unsupported revision state %q", status.State)
	}
	token, err := r.auth.Token(ctx, status.InstallationID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"state": native, "key": "platform-preview", "name": "Platform preview", "description": status.Description, "url": status.TargetURL})
	endpoint := strings.TrimRight(r.baseURL, "/") + "/repositories/" + strings.Trim(status.Repository, "/") + "/commit/" + status.SHA + "/statuses/build"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.Value)
	req.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Bitbucket status API returned %s", response.Status)
	}
	return nil
}
