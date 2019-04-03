package cert

import (
	"context"
	"fmt"
	"time"

	rkecluster "github.com/rancher/rke/cluster"
	"github.com/rancher/rke/cmd"
	"github.com/rancher/rke/hosts"
	"github.com/rancher/rke/k8s"
	"github.com/rancher/rke/log"
	"github.com/rancher/rke/pki"
	"github.com/rancher/system-tools/clients"
	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/cert"
)

func DoRotate(ctx *cli.Context) error {
	clusterName := ctx.String("cluster")

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

	if _, err := getClusterFullState(downstreamClient, clusterName); err == nil {
		logrus.Infof("Cluster [%s] is not a legacy cluster, please use rotate certificate from Rancher UI", clusterName)
		return nil
	}

	externalFlags := rkecluster.GetExternalFlags(false, false, false, "", clusterName)
	externalFlags.Legacy = true
	rkeConfig.RotateCertificates = &v3.RotateCertificates{}
	if err := cmd.ClusterInit(context.Background(), rkeConfig, hosts.DialersOptions{}, externalFlags); err != nil {
		return err
	}
	_, _, _, _, newCerts, err := cmd.ClusterUp(context.Background(), hosts.DialersOptions{}, externalFlags)
	if err != nil {
		return err
	}
	if err := saveClusterCertsToKubernetes(context.Background(), downstreamClient, newCerts); err != nil {
		return err
	}

	return cleanupSetup(ctx, clusterName)
}

func saveClusterCertsToKubernetes(ctx context.Context, kubeClient *kubernetes.Clientset, crts map[string]pki.CertificatePKI) error {
	log.Infof(ctx, "[certificates] Save kubernetes certificates as secrets")
	var errgrp errgroup.Group
	for crtName, crt := range crts {
		name := crtName
		certificate := crt
		errgrp.Go(func() error {
			return saveCertAsSecret(kubeClient, name, certificate)
		})
	}
	if err := errgrp.Wait(); err != nil {
		return err

	}
	log.Infof(ctx, "[certificates] Successfully saved certificates as kubernetes secret [%s]", pki.CertificatesSecretName)
	return nil
}

func saveCertAsSecret(kubeClient *kubernetes.Clientset, crtName string, crt pki.CertificatePKI) error {
	logrus.Debugf("[certificates] Saving certificate [%s] to kubernetes", crtName)
	timeout := make(chan bool, 1)

	// build secret Data
	secretData := make(map[string][]byte)
	if crt.Certificate != nil {
		secretData["Certificate"] = cert.EncodeCertPEM(crt.Certificate)
		secretData["EnvName"] = []byte(crt.EnvName)
		secretData["Path"] = []byte(crt.Path)
	}
	if crt.Key != nil {
		secretData["Key"] = cert.EncodePrivateKeyPEM(crt.Key)
		secretData["KeyEnvName"] = []byte(crt.KeyEnvName)
		secretData["KeyPath"] = []byte(crt.KeyPath)
	}
	if len(crt.Config) > 0 {
		secretData["ConfigEnvName"] = []byte(crt.ConfigEnvName)
		secretData["Config"] = []byte(crt.Config)
		secretData["ConfigPath"] = []byte(crt.ConfigPath)
	}
	go func() {
		for {
			err := k8s.UpdateSecret(kubeClient, secretData, crtName)
			if err != nil {
				time.Sleep(time.Second * 5)
				continue
			}
			timeout <- true
			break
		}
	}()
	select {
	case <-timeout:
		return nil
	case <-time.After(time.Second * 5):
		return fmt.Errorf("[certificates] Timeout waiting for kubernetes to be ready")
	}
}
