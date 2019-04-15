package cert

import (
	"archive/zip"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	rkecluster "github.com/rancher/rke/cluster"
	"github.com/rancher/rke/pki"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

func readZipFile(zf *zip.File) ([]byte, error) {
	f, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

func callRancherAPI(url, token, method string) (*http.Response, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   300 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	token64encoded := base64.StdEncoding.EncodeToString([]byte(token))
	req.Header.Add("Authorization", "Basic "+token64encoded)
	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		content, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("invalid response %d: %s", resp.StatusCode, string(content))
	}
	return resp, nil
}

func writeTempKubeConfig(kubeconfig, clusterName string) error {
	logrus.Infof("Write temporary kubeconfig for cluster [%s]", clusterName)
	kubeConfigPath := pki.GetLocalKubeConfig(clusterName, "")
	if err := ioutil.WriteFile(kubeConfigPath, []byte(kubeconfig), 0640); err != nil {
		return fmt.Errorf("Failed to write temporary kubeconfig file cluster [%s]: %v", kubeConfigPath, err)
	}
	logrus.Infof("Successfully wrote temporary kubeconfig file for cluster [%s] at [%s]", clusterName, kubeConfigPath)
	return nil
}

func WriteTempConfig(config, clusterName string) error {
	logrus.Infof("Write temporary config for cluster [%s]", clusterName)
	if err := ioutil.WriteFile(clusterName, []byte(config), 0640); err != nil {
		return fmt.Errorf("Failed to write temporary config file cluster [%s]: %v", clusterName, err)
	}
	logrus.Infof("Successfully wrote temporary config file for cluster [%s] at [%s]", clusterName, clusterName)
	return nil
}

func cleanupSetup(ctx *cli.Context, clusterName string) error {
	kubeConfigPath := pki.GetLocalKubeConfig(clusterName, "")
	if _, err := os.Stat(kubeConfigPath); err == nil {
		logrus.Infof("clean temporary kubeconfig for cluster [%s]", clusterName)
		if err := os.Remove(kubeConfigPath); err != nil {
			return fmt.Errorf("Failed to clean temporary kubeconfig file cluster [%s]: %v", kubeConfigPath, err)
		}
	}
	statefilePath := rkecluster.GetStateFilePath(clusterName, "")
	if _, err := os.Stat(statefilePath); err == nil {
		logrus.Infof("clean temporary rkestate for cluster [%s]", clusterName)
		if err := os.Remove(statefilePath); err != nil {
			return fmt.Errorf("Failed to clean temporary rkestate file for cluster [%s]: %v", statefilePath, err)
		}
	}
	return nil
}

func addRemoveUserToDockerGroup(address, user, key string, remove bool) (error, string) {
	var errorBuffer bytes.Buffer
	keyName := address + "-key.pem"

	if remove {
		logrus.Infof("Remove user [%s] from docker group on node [%s]", user, address)
	} else {
		logrus.Infof("Adding user [%s] temporarily to docker group on node [%s]", user, address)
	}

	sshCommand := fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no %s@%s ", user, address)
	if key != "" {
		if err := ioutil.WriteFile(keyName, []byte(key), 0600); err != nil {
			return fmt.Errorf("Failed to write temporary ssh key for node [%s]: %v", address, err), ""
		}
		sshCommand = fmt.Sprintf("ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s %s@%s ", keyName, user, address)
	}

	usermodCmd := fmt.Sprintf("sudo usermod -aG docker %s", user)
	if remove {
		usermodCmd = fmt.Sprintf("sudo usermod -G \"\" %s", user)
	}

	cmd := exec.Command("ssh")
	cmd.Args = strings.Split(sshCommand+usermodCmd, " ")
	cmd.Stderr = &errorBuffer
	cmdErr := cmd.Run()
	// remove temp key
	if key != "" {
		if _, err := os.Stat(keyName); err == nil {
			logrus.Infof("clean temporary sshkey for node [%s]", address)
			if err := os.Remove(keyName); err != nil {
				return fmt.Errorf("Failed to clean temporary sshkey for node [%s]: %v", address, err), ""
			}
		}
	}
	return cmdErr, errorBuffer.String()
}
