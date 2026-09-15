package executor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
)

func TestKubernetesConfigUsesInClusterConfig(t *testing.T) {
	want := &rest.Config{Host: "https://in-cluster.example.test"}
	originalInClusterConfig := inClusterConfig
	originalLocalKubeConfig := localKubeConfig
	t.Cleanup(func() {
		inClusterConfig = originalInClusterConfig
		localKubeConfig = originalLocalKubeConfig
	})

	inClusterConfig = func() (*rest.Config, error) { return want, nil }
	localKubeConfig = func() (*rest.Config, error) {
		t.Fatal("local kubeconfig should not be consulted when in-cluster config succeeds")
		return nil, nil
	}

	got, err := kubernetesConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got config %#v, want in-cluster config %#v", got, want)
	}
}

func TestKubernetesConfigFallsBackToKubeconfig(t *testing.T) {
	kubeconfigPath := filepath.Join(t.TempDir(), "config")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: https://local.example.test
contexts:
- name: local
  context:
    cluster: local
    user: local
current-context: local
users:
- name: local
  user: {}
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatal(err)
	}

	originalInClusterConfig := inClusterConfig
	t.Cleanup(func() { inClusterConfig = originalInClusterConfig })
	inClusterConfig = func() (*rest.Config, error) {
		return nil, errors.New("not running in a cluster")
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	got, err := kubernetesConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "https://local.example.test" {
		t.Fatalf("got host %q, want local kubeconfig host", got.Host)
	}
}
