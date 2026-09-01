package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Provider struct {
	client         *http.Client
	baseURL, token string
}

func New() *Provider {
	return &Provider{client: &http.Client{Timeout: 10 * time.Second}, baseURL: "https://api.bitbucket.org/2.0", token: os.Getenv("BITBUCKET_TOKEN")}
}
func (p *Provider) Supports(repoURL string) bool {
	_, _, err := parseRepository(repoURL)
	return err == nil
}
func (p *Provider) ValidateRepo(repoURL string) error {
	workspace, repo, err := parseRepository(repoURL)
	if err != nil {
		return err
	}
	return p.get(fmt.Sprintf("/repositories/%s/%s", workspace, repo), nil)
}
func (p *Provider) DetectProjectType(repoURL string) (string, error) {
	workspace, repo, err := parseRepository(repoURL)
	if err != nil {
		return "", err
	}
	var listing struct {
		Values []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"values"`
	}
	if err := p.get(fmt.Sprintf("/repositories/%s/%s/src/HEAD/?pagelen=100", workspace, repo), &listing); err != nil {
		return "", err
	}
	files := map[string]bool{}
	for _, entry := range listing.Values {
		if entry.Type == "commit_file" {
			files[strings.ToLower(entry.Path)] = true
		}
	}
	switch {
	case files["go.mod"]:
		return "go", nil
	case files["package.json"]:
		return "node", nil
	case files["pyproject.toml"] || files["requirements.txt"] || files["setup.py"]:
		return "python", nil
	case files["pom.xml"] || files["build.gradle"] || files["build.gradle.kts"]:
		return "java", nil
	case files["cargo.toml"]:
		return "rust", nil
	default:
		return "", fmt.Errorf("project type could not be inferred from repository root")
	}
}
func (p *Provider) get(path string, destination interface{}) error {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(p.baseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("query Bitbucket repository: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bitbucket repository query returned %s", resp.Status)
	}
	if destination != nil {
		if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
			return fmt.Errorf("decode Bitbucket response: %w", err)
		}
	}
	return nil
}
func parseRepository(raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "git@bitbucket.org:") {
		value = "https://bitbucket.org/" + strings.TrimPrefix(value, "git@bitbucket.org:")
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Hostname(), "bitbucket.org") {
		return "", "", fmt.Errorf("repository URL must reference bitbucket.org")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repository URL must contain workspace and repository")
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if parts[0] == "" || repo == "" {
		return "", "", fmt.Errorf("repository URL must contain workspace and repository")
	}
	return url.PathEscape(parts[0]), url.PathEscape(repo), nil
}
