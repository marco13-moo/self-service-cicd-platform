package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// Authenticator exchanges Bitbucket Cloud OAuth consumer credentials for
// ephemeral bearer tokens. The workspace identifier is accepted to satisfy the
// neutral contract but is not part of Bitbucket's client-credentials exchange.
type Authenticator struct {
	clientID, clientSecret string
	client                 *http.Client
	tokenURL               string
	mu                     sync.Mutex
	token                  scm.InstallationToken
	now                    func() time.Time
}

func NewAuthenticator(clientID, clientSecret string) *Authenticator {
	return &Authenticator{clientID: clientID, clientSecret: clientSecret, client: &http.Client{Timeout: 10 * time.Second}, tokenURL: "https://bitbucket.org/site/oauth2/access_token", now: time.Now}
}
func (*Authenticator) Provider() scm.Provider { return scm.ProviderBitbucket }
func (a *Authenticator) Token(ctx context.Context, _ string) (scm.InstallationToken, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.clientID == "" || a.clientSecret == "" {
		return scm.InstallationToken{}, scm.ErrNotConfigured
	}
	if a.token.Value != "" && a.token.ExpiresAt.After(a.now().Add(time.Minute)) {
		return a.token, nil
	}
	form := url.Values{"grant_type": []string{"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return scm.InstallationToken{}, err
	}
	req.SetBasicAuth(a.clientID, a.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return scm.InstallationToken{}, fmt.Errorf("exchange Bitbucket OAuth token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return scm.InstallationToken{}, fmt.Errorf("exchange Bitbucket OAuth token: %s", resp.Status)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || payload.AccessToken == "" || payload.ExpiresIn <= 0 {
		return scm.InstallationToken{}, fmt.Errorf("decode Bitbucket OAuth token")
	}
	a.token = scm.InstallationToken{Value: payload.AccessToken, ExpiresAt: a.now().Add(time.Duration(payload.ExpiresIn) * time.Second)}
	return a.token, nil
}
