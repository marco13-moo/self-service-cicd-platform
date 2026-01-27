package providers

// RepositoryProvider defines the interface the control plane uses
// to interact with source code hosts (GitHub, GitLab, Bitbucket, etc).
//
// Implementations are expected to handle authentication, repository
// metadata inspection, and webhook registration.
type RepositoryProvider interface {
	// ValidateRepo verifies the repository exists and is accessible.
	ValidateRepo(repoURL string) error

	// DetectProjectType inspects the repository and returns its build type
	// (e.g. node, python, go).
	DetectProjectType(repoURL string) (string, error)
}
