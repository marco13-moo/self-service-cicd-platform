package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

// Provider performs bounded, read-only repository inspection through GitHub's
// REST API. GITHUB_TOKEN is optional for public repositories.
type Provider struct {
	client  *http.Client
	baseURL string
	token   string
}

func New() *Provider {
	return &Provider{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: defaultAPIBaseURL,
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

func (p *Provider) ValidateRepo(repoURL string) error {
	owner, repo, err := parseRepository(repoURL)
	if err != nil {
		return err
	}
	return p.get(fmt.Sprintf("/repos/%s/%s", owner, repo), nil)
}

func (p *Provider) DetectProjectType(repoURL string) (string, error) {
	owner, repo, err := parseRepository(repoURL)
	if err != nil {
		return "", err
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := p.get(fmt.Sprintf("/repos/%s/%s/contents", owner, repo), &entries); err != nil {
		return "", err
	}
	files := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Type == "file" {
			files[strings.ToLower(entry.Name)] = true
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
		return fmt.Errorf("construct GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("query GitHub repository: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub repository query returned %s", resp.Status)
	}
	if destination != nil {
		if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
			return fmt.Errorf("decode GitHub response: %w", err)
		}
	}
	return nil
}

func parseRepository(raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "git@github.com:") {
		value = "https://github.com/" + strings.TrimPrefix(value, "git@github.com:")
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return "", "", fmt.Errorf("repository URL must reference github.com")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repository URL must contain owner and repository")
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if parts[0] == "" || repo == "" {
		return "", "", fmt.Errorf("repository URL must contain owner and repository")
	}
	return url.PathEscape(parts[0]), url.PathEscape(repo), nil
}
