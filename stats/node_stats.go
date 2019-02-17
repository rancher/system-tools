package stats

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rancher/system-tools/clients"
	"github.com/rancher/system-tools/templates"
	"github.com/rancher/system-tools/utils"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var StatsFlags = []cli.Flag{
	cli.StringFlag{
		Name:   "kubeconfig,c",
		EnvVar: "KUBECONFIG",
		Usage:  "managed cluster kubeconfig",
	},
	cli.StringFlag{
		Name:  "node,n",
		Usage: "show stats for a single node",
	},
	cli.StringFlag{
		Name:  "stats-command,s",
		Usage: "alternative command to run on the servers",
		Value: DefaultStatsCommand,
	},
}

const (
	StatsCollectorDSName      = "stats-collector"
	StatsCollectorDSNamespace = "cattle-system"
	StatsCollectorSelector    = "k8s-app=stats-collector"
	DefaultStatsCommand       = "/usr/bin/sar -u -r -F 1 1"
)

func DoStats(ctx *cli.Context) error {
	statsNode := ctx.String("node")
	statsCommand := ctx.String("stats-command")
	client, err := clients.GetClientSet(ctx)
	if err != nil {
		return err
	}

	restConfig, err := clients.GetRestConfig(ctx)
	if err != nil {
		return err
	}

	if err := deployStatsCollectors(client); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// clean up before you leave
	sigChan := make(chan os.Signal, 1)
	done := false
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logrus.Infof("user interrupt..cleaning up..")
		done = true
	}()
	ownerUID, err := utils.GetCollectorDSUID(client, StatsCollectorDSName, StatsCollectorDSNamespace)
	if err != nil {
		return err
	}
	podList, err := client.CoreV1().Pods(StatsCollectorDSNamespace).List(v1.ListOptions{LabelSelector: StatsCollectorSelector})
	if err != nil {
		return err
	}
	for {
		for _, pod := range podList.Items {
			if done {
				break
			}
			buf := bytes.Buffer{}
			// ignore pods that we didn't run
			nodeName := pod.Spec.NodeName
			if pod.GetOwnerReferences()[0].UID != ownerUID {
				continue
			}
			if len(statsNode) != 0 && nodeName != statsNode {
				continue
			}
			logrus.Infof("node stats for [%s]..", nodeName)
			if err := utils.PodExecCommand(restConfig, pod, []string{"sh", "-c", statsCommand}, &buf); err != nil {
				if strings.Contains(err.Error(), "exit code 127") ||
					strings.Contains(err.Error(), "unable to upgrade connection") {
					logrus.Infof("waiting for collector pod [%s/%s] on [%s] to be ready..", pod.Namespace, pod.Name, pod.Spec.NodeName)
				} else {
					logrus.Warnf("error executing command on pod [%s/%s] on [%s]: %v", pod.Namespace, pod.Name, pod.Spec.NodeName, err)
				}
			}
			fmt.Printf("%s\n\n", buf.String())
		}
		time.Sleep(5 * time.Second)
		if done {
			return deleteStatsCollectors(client)
		}
	}
}

func deployStatsCollectors(client *kubernetes.Clientset) error {
	logrus.Infof("deploying stats collection DaemonSet [%s]..", StatsCollectorDSName)
	dsConfig := map[string]string{}
	agentImage, err := utils.GetClusterAgentImage(client)
	if err != nil {
		return err
	}
	dsConfig["Image"] = agentImage
	dsTmplt, err := utils.CompileTemplateFromMap(templates.StatsDSTemplate, dsConfig)
	if err != nil {
		return err
	}

	statsCollectorDS := &appsv1.DaemonSet{}
	if err := utils.DecodeYamlResource(statsCollectorDS, dsTmplt); err != nil {
		return err
	}
	if _, err := client.AppsV1().DaemonSets(statsCollectorDS.Namespace).Create(statsCollectorDS); err != nil {
		return err
	}
	// make sure the DaemonSet is ready
	logrus.Infof("wating for DaemonSet [%s] to be ready..", StatsCollectorDSName)

	for {
		ds, err := client.AppsV1().DaemonSets(statsCollectorDS.Namespace).Get(statsCollectorDS.Name, v1.GetOptions{})
		if err != nil {
			return err
		}
		if ds.Status.CurrentNumberScheduled != ds.Status.DesiredNumberScheduled {
			time.Sleep(time.Second * 1)
			continue
		}
		break
	}
	logrus.Infof("stats collection DaemonSet [%s] deployed successfully..", StatsCollectorDSName)

	return nil
}

func deleteStatsCollectors(client *kubernetes.Clientset) error {
	logrus.Infof("removing stats collection DaemonSet [%s]..", StatsCollectorDSName)
	dsConfig := map[string]string{}
	agentImage, err := utils.GetClusterAgentImage(client)
	if err != nil {
		return err
	}
	dsConfig["Image"] = agentImage
	dsTmplt, err := utils.CompileTemplateFromMap(templates.StatsDSTemplate, dsConfig)
	if err != nil {
		return err
	}
	statsCollectorDS := &appsv1.DaemonSet{}
	if err := utils.DecodeYamlResource(statsCollectorDS, dsTmplt); err != nil {
		return err
	}
	if err := client.AppsV1().DaemonSets(statsCollectorDS.Namespace).Delete(statsCollectorDS.Name, &v1.DeleteOptions{}); err != nil {
		return err
	}

	logrus.Infof("stats collection DaemonSet [%s] removed successfully..", StatsCollectorDSName)
	return nil
}
