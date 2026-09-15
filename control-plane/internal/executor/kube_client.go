package executor

import (
	"fmt"

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
	return kubernetesConfig()
}

var inClusterConfig = rest.InClusterConfig

var localKubeConfig = func() (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

func kubernetesConfig() (*rest.Config, error) {
	if cfg, err := inClusterConfig(); err == nil {
		return cfg, nil
	}

	return localKubeConfig()
}
