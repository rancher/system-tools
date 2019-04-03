package clients

import (
	"fmt"

	"github.com/urfave/cli"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetClientSet(ctx *cli.Context) (*kubernetes.Clientset, error) {
	config, err := GetRestConfig(ctx)
	if err != nil {
		return nil, err
	}
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func GetRestConfig(ctx *cli.Context) (*rest.Config, error) {
	kubeconfig := ctx.String("kubeconfig")
	if len(kubeconfig) == 0 {
		return nil, fmt.Errorf("Please use option -c to set a kubeconfig file")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func GetGroupDynamicClient(ctx *cli.Context, gvStr string) (dynamic.Interface, error) {
	restConfig, err := GetRestConfig(ctx)
	if err != nil {
		return nil, err
	}
	gv, err := schema.ParseGroupVersion(gvStr)
	if err != nil {
		return nil, err
	}
	if len(gv.Group) != 0 {
		restConfig.APIPath = "/apis/"
	}
	restConfig.GroupVersion = &gv

	return dynamic.NewForConfig(restConfig)
}

func GetDiscoveryClient(ctx *cli.Context) (*discovery.DiscoveryClient, error) {
	clientSet, err := GetClientSet(ctx)
	if err != nil {
		return nil, err
	}
	return clientSet.DiscoveryClient, nil
}

func GetAPIExtensionsClient(ctx *cli.Context) (*clientset.Clientset, error) {
	resetConfig, err := GetRestConfig(ctx)
	if err != nil {
		return nil, err
	}
	return clientset.NewForConfig(resetConfig)

}

func GetCustomClientSet(kubeconfig string) (*kubernetes.Clientset, error) {
	if len(kubeconfig) == 0 {
		return nil, fmt.Errorf("cluster Kubeconfig is empty")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	// create the clientset
	return kubernetes.NewForConfig(config)
}
