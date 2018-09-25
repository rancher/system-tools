package logs

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/rancher/system-tools/clients"
	"github.com/rancher/system-tools/templates"
	"github.com/rancher/system-tools/utils"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	LogCollectorDSName      = "log-collector"
	LogCollectorDSNamespace = "cattle-system"
	LogCollectorSelector    = "k8s-app=log-collector"
)

var LogFlags = []cli.Flag{
	cli.StringFlag{
		Name:   "kubeconfig,c",
		EnvVar: "KUBECONFIG",
		Usage:  "managed cluster kubeconfig",
	},
	cli.StringFlag{
		Name:  "output,o",
		Usage: "cluster logs tarball",
		Value: "cluster-logs.tar",
	},
	cli.StringFlag{
		Name:  "node,n",
		Usage: "fetch logs for a single node",
	},
}

func DoLogs(ctx *cli.Context) error {
	logTarball := ctx.String("output")
	if len(logTarball) == 0 {
		return fmt.Errorf("Please chose an output file name for the logs tarball")
	}
	fetchNode := ctx.String("node")

	client, err := clients.GetClientSet(ctx)
	if err != nil {
		return err
	}
	restConfig, err := clients.GetRestConfig(ctx)
	if err != nil {
		return err
	}
	// check if we support this cluster, we only support RKE at the moment
	_, err = utils.GetClusterProvider(client)
	if err != nil {
		return err
	}

	if err := deployLogCollectors(client); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	defer deleteLogCollectors(client)

	podList, err := client.CoreV1().Pods(LogCollectorDSNamespace).List(v1.ListOptions{LabelSelector: LogCollectorSelector})
	if err != nil {
		return err
	}
	f, err := os.Create(logTarball)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := bytes.Buffer{}
	// fetch log files
	ownerUID, err := getLogCollectorDSUID(client)
	if err != nil {
		return err
	}
	logrus.Infof("starting log collection..")
	for _, pod := range podList.Items {
		// ignore pods that we didn't run
		if pod.GetOwnerReferences()[0].UID != ownerUID {
			continue
		}
		nodeName := pod.Spec.NodeName
		if len(fetchNode) != 0 && nodeName != fetchNode {
			continue
		}
		logrus.Infof("fetching logs from node [%s]..", nodeName)
		fileName := fmt.Sprintf("%s.tar", nodeName)

		if err := utils.ReadFileFromPod(restConfig, pod, path.Join("/tmp/", fileName), &buf); err != nil {
			return err
		}
		if err := utils.AddToTarBall(f, &buf); err != nil {
			return err
		}
		f.Sync()
	}
	return nil
}

func deployLogCollectors(client *kubernetes.Clientset) error {
	logrus.Infof("deploying log collection DaemonSet [%s]..", LogCollectorDSName)

	dsConfig := map[string]string{}
	agentImage, err := utils.GetClusterAgentImage(client)
	if err != nil {
		return err
	}
	dsConfig["Image"] = agentImage
	dsTmplt, err := utils.CompileTemplateFromMap(templates.LogCollectorDSTemplate, dsConfig)
	if err != nil {
		return err
	}

	logCollectorDS := &appsv1.DaemonSet{}
	if err := utils.DecodeYamlResource(logCollectorDS, dsTmplt); err != nil {
		return err
	}
	if _, err := client.AppsV1().DaemonSets(logCollectorDS.Namespace).Create(logCollectorDS); err != nil {
		return err
	}
	// make sure the DaemonSet is ready
	logrus.Infof("wating for DaemonSet [%s] to be ready..", LogCollectorDSName)

	for {
		ds, err := client.AppsV1().DaemonSets(logCollectorDS.Namespace).Get(logCollectorDS.Name, v1.GetOptions{})
		if err != nil {
			return err
		}
		if ds.Status.CurrentNumberScheduled != ds.Status.DesiredNumberScheduled {
			time.Sleep(time.Second * 1)
			continue
		}
		break
	}
	logrus.Infof("log collection DaemonSet [%s] deployed successfully..", LogCollectorDSName)

	return nil
}

func deleteLogCollectors(client *kubernetes.Clientset) error {
	logrus.Infof("removing log collection DaemonSet [%s]..", LogCollectorDSName)
	dsConfig := map[string]string{}
	agentImage, err := utils.GetClusterAgentImage(client)
	if err != nil {
		return err
	}
	dsConfig["Image"] = agentImage
	dsTmplt, err := utils.CompileTemplateFromMap(templates.LogCollectorDSTemplate, dsConfig)
	if err != nil {
		return err
	}

	logCollectorDS := &appsv1.DaemonSet{}
	if err := utils.DecodeYamlResource(logCollectorDS, dsTmplt); err != nil {
		return err
	}
	if err := client.AppsV1().DaemonSets(logCollectorDS.Namespace).Delete(logCollectorDS.Name, &v1.DeleteOptions{}); err != nil {
		return err
	}
	logrus.Infof("log collection DaemonSet [%s] removed successfully..", LogCollectorDSName)
	return nil
}

func getLogCollectorDSUID(client *kubernetes.Clientset) (types.UID, error) {
	ds, err := client.AppsV1().DaemonSets(LogCollectorDSNamespace).Get(LogCollectorDSName, v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return ds.GetUID(), nil
}
