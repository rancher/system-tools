package config

import (
	"github.com/rancher/system-tools/cert"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	yaml "gopkg.in/yaml.v2"
)

var ConfigFlags = append(cert.CertFlags, cli.BoolFlag{
	Name:  "print",
	Usage: "only print config on the screen",
})

func DoConfig(ctx *cli.Context) error {
	clusterName := ctx.String("cluster")

	logrus.Infof("Generate cluster config for cluster [%s]", clusterName)
	rkeConfig, err := cert.SetupRancherKubernetesEngineConfig(ctx, clusterName)
	if err != nil {
		return err
	}
	rkeConfigYaml, err := yaml.Marshal(rkeConfig)
	if err != nil {
		return err
	}
	if ctx.Bool("print") {
		logrus.Infof("Config for cluster [%s]:\n%s", clusterName, string(rkeConfigYaml))
		return nil
	}
	return cert.WriteTempConfig(string(rkeConfigYaml), clusterName)
}
