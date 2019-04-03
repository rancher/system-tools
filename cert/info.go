package cert

import (
	"context"
	"fmt"

	rkecluster "github.com/rancher/rke/cluster"
	"github.com/rancher/rke/hosts"
	"github.com/rancher/rke/pki"
	"github.com/rancher/system-tools/clients"
	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/cert"
)

func DoInfo(ctx *cli.Context) error {
	clusterName := ctx.String("cluster")

	logrus.Infof("Check certificates Info for cluster [%s]", clusterName)
	rkeConfig, err := SetupRancherKubernetesEngineConfig(ctx, clusterName)
	if err != nil {
		return err
	}

	clusterKubeConfig, err := getClusterKubeConfigFromAPI(ctx, clusterName)
	if err != nil {
		return err
	}

	if err := writeTempKubeConfig(clusterKubeConfig, clusterName); err != nil {
		return err
	}
	downstreamClient, err := clients.GetCustomClientSet(pki.GetLocalKubeConfig(clusterName, ""))
	if err != nil {
		return err
	}

	if err := showCertificatesInfo(downstreamClient, clusterName, rkeConfig); err != nil {
		return err
	}

	return cleanupSetup(ctx, clusterName)
}

func showCertificatesInfo(k8sClient *kubernetes.Clientset, clusterName string, rkeconfig *v3.RancherKubernetesEngineConfig) error {
	var certMap map[string]pki.CertificatePKI
	var nodes []v3.RKEConfigNode

	clusterFullState, err := getClusterFullState(k8sClient, clusterName)
	if err == nil {
		certMap = clusterFullState.CurrentState.CertificatesBundle
		nodes = clusterFullState.CurrentState.RancherKubernetesEngineConfig.Nodes
	} else {
		certMap, err = getCertsFromLegacyCluster(clusterName, rkeconfig)
		if err != nil {
			return err
		}
		nodes = rkeconfig.Nodes
	}

	componentsCerts := []string{
		pki.KubeAPICertName,
		pki.KubeControllerCertName,
		pki.KubeSchedulerCertName,
		pki.KubeProxyCertName,
		pki.KubeNodeCertName,
		pki.KubeAdminCertName,
		pki.RequestHeaderCACertName,
		pki.APIProxyClientCertName,
	}
	etcdHosts := hosts.NodesToHosts(nodes, "etcd")
	for _, host := range etcdHosts {
		etcdName := pki.GetEtcdCrtName(host.InternalAddress)
		componentsCerts = append(componentsCerts, etcdName)
	}
	for _, component := range componentsCerts {
		componentCert := certMap[component]
		if componentCert.CertificatePEM != "" {
			certificates, err := cert.ParseCertsPEM([]byte(componentCert.CertificatePEM))
			if err != nil {
				return fmt.Errorf("failed to read certificate [%s]: %v", component, err)
			}
			certificate := certificates[0]
			logrus.Infof("Certificate [%s] has expiration date: [%v]", component, certificate.NotAfter)
		}
	}
	return nil
}

func getCertsFromLegacyCluster(clusterName string, rkeconfig *v3.RancherKubernetesEngineConfig) (map[string]pki.CertificatePKI, error) {
	logrus.Infof("possible legacy cluster, trying to fetch certs from kubernetes")
	externalFlags := rkecluster.GetExternalFlags(false, false, false, "", clusterName)
	rkeClusterObj, err := rkecluster.InitClusterObject(context.Background(), rkeconfig, externalFlags)
	if err != nil {
		return nil, err
	}
	return rkecluster.GetClusterCertsFromKubernetes(context.Background(), rkeClusterObj)
}
