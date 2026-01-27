package github

import "fmt"

// Provider is a placeholder GitHub implementation of RepositoryProvider.
// Real logic will be added in Phase 4.
type Provider struct{}

func New() *Provider {
	return &Provider{}
}

func (p *Provider) ValidateRepo(repoURL string) error {
	return fmt.Errorf("github provider not implemented")
}

func (p *Provider) DetectProjectType(repoURL string) (string, error) {
	return "", fmt.Errorf("github provider not implemented")
}
