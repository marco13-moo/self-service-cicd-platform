package executor

import (
	"fmt"
	"os"

	argoclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Clients struct {
	Argo argoclient.Interface
}

func NewClients() (*Clients, error) {

	cfg, err := buildConfig()
	if err != nil {
		return nil, fmt.Errorf("build kube config: %w", err)
	}

	argo, err := argoclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create argo client: %w", err)
	}

	return &Clients{
		Argo: argo,
	}, nil
}

func buildConfig() (*rest.Config, error) {

	// Production path
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	// Local development fallback
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.ExpandEnv("$HOME/.kube/config")
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
