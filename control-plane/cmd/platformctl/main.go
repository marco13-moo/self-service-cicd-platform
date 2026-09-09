// platformctl is the intentionally thin developer interface to the
// provider-neutral platform API. Business policy remains server-side.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"gopkg.in/yaml.v3"
)

type serviceManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Owner       string `yaml:"owner"`
		Repository  string `yaml:"repository"`
		Environment string `yaml:"environment"`
		Deployment  struct {
			ContainerPort int    `yaml:"containerPort"`
			Dockerfile    string `yaml:"dockerfile"`
			Egress        []struct {
				DNSName  string `yaml:"dnsName"`
				Port     int    `yaml:"port"`
				Protocol string `yaml:"protocol"`
			} `yaml:"egress"`
		} `yaml:"deployment"`
	} `yaml:"spec"`
}

func main() {
	endpoint := flag.String("endpoint", envOr("PLATFORM_ENDPOINT", "http://localhost:8080"), "platform API URL")
	token := flag.String("token", os.Getenv("PLATFORM_TOKEN"), "OIDC bearer token (prefer PLATFORM_TOKEN)")
	file := flag.String("file", "", "service declaration for apply")
	flag.Parse()
	if err := execute(*endpoint, *token, *file, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "platformctl:", err)
		os.Exit(1)
	}
}

func execute(endpoint, token, file string, args []string) error {
	if len(args) == 0 {
		return errors.New("expected apply, catalog, or diagnose SERVICE")
	}
	method, path, body := http.MethodGet, "", []byte(nil)
	switch args[0] {
	case "apply":
		if file == "" {
			return errors.New("apply requires -file")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var manifest serviceManifest
		if err = yaml.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("decode declaration: %w", err)
		}
		if manifest.APIVersion != "platform.service/v1" || manifest.Kind != "Service" {
			return errors.New("unsupported apiVersion or kind")
		}
		request := api.CreateServiceRequest{Name: manifest.Metadata.Name, Owner: manifest.Spec.Owner, RepoURL: manifest.Spec.Repository, Environment: manifest.Spec.Environment,
			Deployment: &api.ServiceDeploymentRequest{ContainerPort: manifest.Spec.Deployment.ContainerPort, Dockerfile: manifest.Spec.Deployment.Dockerfile}}
		for _, rule := range manifest.Spec.Deployment.Egress {
			request.Deployment.Egress = append(request.Deployment.Egress, api.ServiceEgressRule{DNSName: rule.DNSName, Port: rule.Port, Protocol: rule.Protocol})
		}
		body, err = json.Marshal(request)
		if err != nil {
			return err
		}
		method, path = http.MethodPost, "/api/v1/services"
	case "catalog":
		path = "/api/v1/catalog/services"
	case "diagnose":
		if len(args) != 2 {
			return errors.New("diagnose requires exactly one service name")
		}
		path = "/api/v1/services/" + args[1] + "/diagnostics"
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	req, err := http.NewRequest(method, strings.TrimRight(endpoint, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("API returned %s: %s", response.Status, strings.TrimSpace(string(result)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, result, "", "  ") == nil {
		result = pretty.Bytes()
	}
	fmt.Println(string(result))
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
