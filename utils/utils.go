package utils

import (
	"archive/tar"
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

const (
	RKECluster = "RKE"
)

func RetryTo(f func() error) error {
	timeout := time.After(time.Second * 60)
	step := time.Tick(time.Second * 2)
	var err error
	for {
		select {
		case <-step:
			if err = f(); err != nil {
				if errors.IsConflict(err) {
					continue
				}
				return err
			}
			return nil
		case <-timeout:
			return fmt.Errorf("Timout error, please try again:%v", err)
		}
	}
}

func RetryWithCount(f func() error, c int) error {
	var err error
	for i := 0; i < c; i++ {
		if err = f(); err != nil && !errors.IsNotFound(err) {
			time.Sleep(2 * time.Second)
			continue
		}
		return nil
	}
	return err
}

func isRKENode(node *corev1.Node) bool {
	for k := range node.Annotations {
		if strings.Contains(k, "rke.cattle.io") {
			return true
		}
	}
	return false
}

func isGEKNode(node *corev1.Node) bool {
	for k := range node.Labels {
		if strings.Contains(k, "cloud.google.com") {
			return true
		}
	}
	return false
}

func DecodeYamlResource(resource interface{}, yamlManifest string) error {
	decoder := yamlutil.NewYAMLToJSONDecoder(bytes.NewReader([]byte(yamlManifest)))
	return decoder.Decode(&resource)
}

func ReadFileFromPod(restConfig *rest.Config, pod corev1.Pod, fileName string, data io.Writer) error {
	if err := PodExecCommand(restConfig, pod, []string{"/bin/cat", fileName}, data); err != nil {
		return fmt.Errorf("error executing command on pod [%s/%s]: %v", pod.Namespace, pod.Name, err)
	}
	return nil
}

func PodExecCommand(restConfig *rest.Config, pod corev1.Pod, command []string, stdout io.Writer) error {
	restClient, err := corev1client.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	req := restClient.RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: pod.Spec.Containers[0].Name,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	execCmd, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return err
	}
	err = execCmd.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: stdout,
		Stderr: ioutil.Discard, // I don't need stderr, exec would give me exit status if something broke
		Tty:    true,
	})
	if err != nil {
		return err
	}
	return nil
}

func GetClusterProvider(client *kubernetes.Clientset) (string, error) {
	// there is no direct way to get Cluster provider, so we use node metadata to figure that out.
	nodeList, err := client.CoreV1().Nodes().List(v1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, node := range nodeList.Items {
		if isRKENode(&node) {
			return RKECluster, nil
		}
	}
	return "", fmt.Errorf("can't figure out cluster provider")
}

func GetClusterAgentImage(client *kubernetes.Clientset) (string, error) {
	podList, err := client.CoreV1().Pods("cattle-system").List(v1.ListOptions{LabelSelector: "app=cattle-agent"})
	if err != nil {
		return "", err
	}

	for _, pod := range podList.Items {
		agentImage := pod.Spec.Containers[0].Image
		return agentImage, nil
	}
	return "", fmt.Errorf("can't find node agent image on this cluster")
}

func AddToTarBall(w io.Writer, r io.Reader) error {
	tr := tar.NewReader(r)
	tw := tar.NewWriter(w)
	for {
		h, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
		// always flush
		tw.Flush()
	}
	return nil
}

func CompileTemplateFromMap(tmplt string, configMap interface{}) (string, error) {
	out := new(bytes.Buffer)
	t := template.Must(template.New("compiled_template").Parse(tmplt))
	if err := t.Execute(out, configMap); err != nil {
		return "", err
	}
	return out.String(), nil
}

func GetCollectorDSUID(client *kubernetes.Clientset, name, namespace string) (types.UID, error) {
	ds, err := client.AppsV1().DaemonSets(namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return ds.GetUID(), nil
}
