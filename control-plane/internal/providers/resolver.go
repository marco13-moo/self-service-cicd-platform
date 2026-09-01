package providers

import "fmt"

type HostAwareRepositoryProvider interface {
	RepositoryProvider
	Supports(repoURL string) bool
}

type Resolver struct{ providers []HostAwareRepositoryProvider }

func NewResolver(providers ...HostAwareRepositoryProvider) *Resolver {
	return &Resolver{providers: providers}
}

func (r *Resolver) ValidateRepo(repoURL string) error {
	provider, err := r.resolve(repoURL)
	if err != nil {
		return err
	}
	return provider.ValidateRepo(repoURL)
}
func (r *Resolver) DetectProjectType(repoURL string) (string, error) {
	provider, err := r.resolve(repoURL)
	if err != nil {
		return "", err
	}
	return provider.DetectProjectType(repoURL)
}
func (r *Resolver) resolve(repoURL string) (HostAwareRepositoryProvider, error) {
	for _, provider := range r.providers {
		if provider.Supports(repoURL) {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("unsupported repository provider")
}
