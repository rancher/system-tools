package cert

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rancher/norman/types/convert"
	rkecluster "github.com/rancher/rke/cluster"
	"github.com/rancher/rke/k8s"
	"github.com/rancher/types/apis/management.cattle.io/v3"
	clientv3 "github.com/rancher/types/client/management/v3"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/client-go/kubernetes"
)

const (
	NamespaceCattleSystem  = "cattle-system"
	KubeConfigTempPath     = "./cluster_kubeconfig.yml"
	FullStateConfigMapName = "full-cluster-state"
)

var CertFlags = []cli.Flag{
	cli.StringFlag{
		Name:   "token,t",
		EnvVar: "TOKEN",
		Usage:  "Rancher server token",
	},
	cli.StringFlag{
		Name:   "url,u",
		EnvVar: "URL",
		Usage:  "Rancher server api url",
	},
	cli.StringFlag{
		Name:  "cluster",
		Usage: "user cluster name",
	},
	cli.StringFlag{
		Name:  "config",
		Usage: "cluster config file",
	},
}

func SetupRancherKubernetesEngineConfig(ctx *cli.Context, clusterName string) (*v3.RancherKubernetesEngineConfig, error) {
	logrus.Infof("Setup rkeconfig for cluster [%s]", clusterName)
	configFile := ctx.String("config")
	if configFile != "" {
		return readClusterConfigFile(configFile)
	}
	// Get Cluster by name
	rkeConfig, err := getRKEConfigFromAPI(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	if rkeConfig == nil {
		return nil, fmt.Errorf("The cluster [%s] isn't an RKE cluster", clusterName)
	}

	return rkeConfig, nil
}

func getRKEConfigFromAPI(ctx *cli.Context, clusterName string) (*v3.RancherKubernetesEngineConfig, error) {
	url := ctx.String("url")
	token := ctx.String("token")

	fullURL := url + "/clusters/" + clusterName

	resp, err := callRancherAPI(fullURL, token, http.MethodGet)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	cluster := &clientv3.Cluster{}
	if err := json.NewDecoder(resp.Body).Decode(cluster); err != nil {
		return nil, err
	}

	for i, node := range cluster.AppliedSpec.RancherKubernetesEngineConfig.Nodes {
		sshKey, err := getSSHKeyFromAPI(ctx, node.NodeID, clusterName)
		if err != nil {
			logrus.Warnf("Failed to get ssh key for node [%s], possible custom node", node.NodeID)
			continue
		}
		cluster.AppliedSpec.RancherKubernetesEngineConfig.Nodes[i].SSHKey = sshKey
	}
	rkeconfig := v3.RancherKubernetesEngineConfig{}
	convert.ToObj(cluster.AppliedSpec.RancherKubernetesEngineConfig, &rkeconfig)
	return &rkeconfig, nil
}

func getSSHKeyFromAPI(ctx *cli.Context, nodeName, clusterName string) (string, error) {
	url := ctx.String("url")
	token := ctx.String("token")
	fullURL := url + "/nodes/" + nodeName + "/nodeconfig"

	resp, err := callRancherAPI(fullURL, token, http.MethodGet)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", err
	}

	for _, zipFile := range zipReader.File {
		if strings.HasSuffix(zipFile.Name, "id_rsa") {
			unzippedFileBytes, err := readZipFile(zipFile)
			if err != nil {
				return "", err
			}
			return string(unzippedFileBytes), nil
		}
		continue
	}
	return "", nil
}

func getClusterKubeConfigFromAPI(ctx *cli.Context, clusterName string) (string, error) {
	logrus.Infof("Get kubeconfig for cluster [%s]", clusterName)

	url := ctx.String("url")
	token := ctx.String("token")
	fullURL := url + "/clusters/" + clusterName + "?action=generateKubeconfig"

	resp, err := callRancherAPI(fullURL, token, http.MethodPost)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	kubeconfigOutput := v3.GenerateKubeConfigOutput{}
	if err := json.NewDecoder(resp.Body).Decode(&kubeconfigOutput); err != nil {
		return "", nil
	}

	return kubeconfigOutput.Config, nil
}

func getClusterFullState(k8sClient *kubernetes.Clientset, clusterName string) (*rkecluster.FullState, error) {
	logrus.Infof("Fetching cluster [%s] full state from kubernetes", clusterName)
	fullStateConfigMap, err := k8s.GetConfigMap(k8sClient, FullStateConfigMapName)
	if err != nil {
		return nil, err
	}
	fullState := rkecluster.FullState{}
	fullStateDate := fullStateConfigMap.Data[FullStateConfigMapName]
	err = json.Unmarshal([]byte(fullStateDate), &fullState)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal cluster state")
	}
	return &fullState, nil
}

func readClusterConfigFile(clusterConfigPath string) (*v3.RancherKubernetesEngineConfig, error) {
	fp, err := filepath.Abs(clusterConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup current directory name: %v", err)
	}
	file, err := os.Open(fp)
	if err != nil {
		return nil, fmt.Errorf("can not find cluster configuration file: %v", err)
	}
	defer file.Close()
	buf, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}
	clusterFileBuff := string(buf)

	rkeConfig := &v3.RancherKubernetesEngineConfig{}
	if err := yaml.Unmarshal([]byte(clusterFileBuff), rkeConfig); err != nil {
		return nil, err
	}
	return rkeConfig, nil
}
