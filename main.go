package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/rancher/types/config"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	CattleControllerName   = "controller.cattle.io"
	DefaultCattleNamespace = "cattle-system"
	CattleLabelBase        = "cattle.io"
)

var VERSION = "dev"

var staticClusterRoles = []string{
	"cluster-owner",
	"create-ns",
	"project-owner",
	"project-owner-promoted",
}
var cattleListOptions = v1.ListOptions{
	LabelSelector: "cattle.io/creator=norman",
}
var deletePolicy = v1.DeletePropagationOrphan
var deleteGracePeriod int64 = 120

func main() {
	commonFlags := []cli.Flag{
		cli.StringFlag{
			Name:   "kubeconfig,c",
			EnvVar: "KUBECONFIG",
			Usage:  "kubeconfig absolute path",
		},
		cli.StringFlag{
			Name:  "namespace,n",
			Value: DefaultCattleNamespace,
			Usage: "rancher 2.x deployment namespace. default is `cattle-system`",
		},
	}
	app := cli.NewApp()
	app.Name = "system-tools"
	app.Version = VERSION
	app.Usage = "Rancher 2.0 operations tool kit"
	app.Commands = []cli.Command{
		cli.Command{
			Name:   "remove",
			Usage:  "safely remove rancher 2.x management plane",
			Action: doRemoveRancher,
			Flags:  commonFlags,
		},
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func doRemoveRancher(ctx *cli.Context) error {
	cattleNamespace := ctx.String("namespace")
	logrus.Infof("Removing rancher deployment in namespace: [%s]", cattleNamespace)
	// setup
	logrus.Infof("Getting conenction configuration")
	restConfig, err := getRestConfig(ctx)
	if err != nil {
		return err
	}
	management, err := config.NewManagementContext(*restConfig)
	if err != nil {
		return err
	}
	k8sClient, err := getClientSet(ctx)
	if err != nil {
		return err
	}

	if err := removeCattleDeployment(ctx); err != nil {
		return err
	}

	if err := removeCattleFinalizers(ctx); err != nil {
		return err
	}

	if err := namespacesCleanup(k8sClient); err != nil {
		return err
	}

	if err := secretsCleanup(k8sClient); err != nil {
		return err
	}

	if err := projectsCleanup(management, k8sClient); err != nil {
		return err
	}

	if err = nodesCleanup(management); err != nil {
		return err
	}

	if err := clustersCleanup(management, k8sClient); err != nil {
		return err
	}

	if err := usersCleanup(management, k8sClient); err != nil {
		return err
	}

	if err := clusterRoleBindginsCleanup(k8sClient); err != nil {
		return err
	}

	if err := clusterRolesCleanup(k8sClient); err != nil {
		return err
	}
	// final cleanup
	logrus.Infof("Removing Rancher Namespace [%s]", cattleNamespace)
	if err := deleteNamespace(k8sClient, cattleNamespace); err != nil && !errors.IsNotFound(err) {
		return err
	}
	logrus.Infof("Successfully removed namespace [%s]", cattleNamespace)
	logrus.Infof("Rancher Management Plane removed successfully")
	return nil
}

func removeCattleDeployment(ctx *cli.Context) error {
	logrus.Infof("Removing Cattle deployment")
	clientSet, err := getClientSet(ctx)
	if err != nil {
		return err
	}
	cattleNamespace := ctx.String("namespace")
	deployments, err := clientSet.AppsV1().Deployments(cattleNamespace).List(v1.ListOptions{})
	if err != nil {
		return err
	}
	for _, deployment := range deployments.Items {
		if err := clientSet.AppsV1().Deployments(cattleNamespace).Delete(deployment.Name, &v1.DeleteOptions{}); err != nil {
			return err
		}
	}
	logrus.Infof("Removed Cattle deployment succuessfully")
	return nil
}
func getClientSet(ctx *cli.Context) (*kubernetes.Clientset, error) {
	config, _ := getRestConfig(ctx)
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func getRestConfig(ctx *cli.Context) (*rest.Config, error) {
	kubeconfig := ctx.String("kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func getGroupDynamicClient(ctx *cli.Context, gvStr string) (*dynamic.Client, error) {
	restConfig, err := getRestConfig(ctx)
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
	return dynamic.NewClient(restConfig)
}

func getDiscoveryClient(ctx *cli.Context) (*discovery.DiscoveryClient, error) {
	clientSet, err := getClientSet(ctx)
	if err != nil {
		return nil, err
	}
	return clientSet.DiscoveryClient, nil
}

func retryTo(f func() error) error {
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

func getProjectList(mgmtCtx *config.ManagementContext) ([]v3.Project, error) {
	projectList, err := mgmtCtx.Management.Projects("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return projectList.Items, nil
}

func getUserList(mgmtCtx *config.ManagementContext) ([]v3.User, error) {
	userList, err := mgmtCtx.Management.Users("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return userList.Items, nil
}

func getNodesList(mgmtCtx *config.ManagementContext) ([]v3.Node, error) {
	nodesList, err := mgmtCtx.Management.Nodes("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return nodesList.Items, nil
}

func getClusterList(mgmtCtx *config.ManagementContext) ([]v3.Cluster, error) {
	clusterList, err := mgmtCtx.Management.Clusters("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return clusterList.Items, nil
}

func deleteProject(mgmtCtx *config.ManagementContext, project v3.Project) error {
	return mgmtCtx.Management.Projects(project.Namespace).Delete(project.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: &deleteGracePeriod,
	})

}

func deleteCluster(mgmtCtx *config.ManagementContext, cluster v3.Cluster) error {

	return mgmtCtx.Management.Clusters("").Delete(cluster.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: &deleteGracePeriod,
	})
}

func deleteUser(mgmtCtx *config.ManagementContext, user v3.User) error {

	return mgmtCtx.Management.Users("").Delete(user.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteNode(mgmtCtx *config.ManagementContext, node v3.Node) error {
	return mgmtCtx.Management.Nodes("local").Delete(node.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: &deleteGracePeriod,
	})
}
func deleteNamespace(client *kubernetes.Clientset, name string) error {
	return retryTo(func() error {
		return client.CoreV1().Namespaces().Delete(name, &v1.DeleteOptions{
			PropagationPolicy:  &deletePolicy,
			GracePeriodSeconds: new(int64),
		})
	})
}

func deleteClusterRole(client *kubernetes.Clientset, name string) error {
	return client.RbacV1().ClusterRoles().Delete(name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteClusterRoleBinding(client *kubernetes.Clientset, name string) error {

	return client.RbacV1().ClusterRoleBindings().Delete(name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func getCattleClusterRoleBindingsList(client *kubernetes.Clientset) ([]string, error) {
	crbList, err := client.RbacV1().ClusterRoleBindings().List(cattleListOptions)
	if err != nil {
		return nil, err
	}
	crbNames := []string{}
	for _, crb := range crbList.Items {
		crbNames = append(crbNames, crb.Name)
	}

	return crbNames, nil
}

func getCattleClusterRolesList(client *kubernetes.Clientset) ([]string, error) {
	crList, err := client.RbacV1().ClusterRoles().List(cattleListOptions)
	if err != nil {
		return nil, err
	}
	crNames := []string{}
	for _, cr := range crList.Items {
		crNames = append(crNames, cr.Name)
	}
	return crNames, nil
}

func cleanupFinalizers(finalizers []string) []string {
	updatedFinalizers := []string{}
	for _, f := range finalizers {
		if strings.Contains(f, CattleControllerName) {
			continue
		}
		updatedFinalizers = append(updatedFinalizers, f)
	}
	return updatedFinalizers
}

func cleanupAnnotationsLabels(m map[string]string) map[string]string {
	for k := range m {
		if strings.Contains(k, CattleLabelBase) {
			delete(m, k)
		}
	}
	return m
}
func getNamespacesList(client *kubernetes.Clientset) ([]string, error) {

	nsList, err := client.CoreV1().Namespaces().List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	nsNames := []string{}
	for _, ns := range nsList.Items {
		nsNames = append(nsNames, ns.Name)
	}
	return nsNames, nil
}

func nodesCleanup(management *config.ManagementContext) error {
	logrus.Infof("Removing machines")
	nodes, err := getNodesList(management)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := deleteNode(management, node); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed Machines")
	return nil
}

func projectsCleanup(management *config.ManagementContext, k8sClient *kubernetes.Clientset) error {
	logrus.Infof("Removing Projects")
	projects, err := getProjectList(management)
	if err != nil {
		return err
	}

	for _, project := range projects {
		logrus.Infof("deleting project [%s]..", project.Name)
		if err := deleteNamespace(k8sClient, project.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteProject(management, project); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed Projects")
	return nil
}

func clustersCleanup(management *config.ManagementContext, k8sClient *kubernetes.Clientset) error {
	logrus.Infof("Removing Clusters")
	clusters, err := getClusterList(management)
	if err != nil {
		return err
	}
	for _, cluster := range clusters {
		logrus.Infof("deleting cluster [%s]..", cluster.Name)
		if err := deleteNamespace(k8sClient, cluster.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteCluster(management, cluster); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed Clusters")
	return nil
}

func usersCleanup(management *config.ManagementContext, k8sClient *kubernetes.Clientset) error {
	logrus.Infof("Removing Users")
	users, err := getUserList(management)
	if err != nil {
		return err
	}
	for _, user := range users {
		logrus.Infof("deleting user [%s]..", user.Name)
		if err := deleteNamespace(k8sClient, user.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteUser(management, user); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed Users")
	return nil
}

func clusterRolesCleanup(k8sClient *kubernetes.Clientset) error {
	logrus.Infof("Removing ClusterRoles")
	clusterRoles, err := getCattleClusterRolesList(k8sClient)
	if err != nil {
		return err
	}
	clusterRoles = append(clusterRoles, staticClusterRoles...)
	for _, clusterRole := range clusterRoles {
		logrus.Infof("deleting cluster role [%s]..", clusterRole)
		if err := deleteClusterRole(k8sClient, clusterRole); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed ClusterRoles")
	return nil
}

func clusterRoleBindginsCleanup(k8sClient *kubernetes.Clientset) error {
	logrus.Infof("Removing ClusterRoleBindings")
	clusterRoleBindings, err := getCattleClusterRoleBindingsList(k8sClient)
	if err != nil {
		return err
	}

	for _, clusterRoleBinding := range clusterRoleBindings {
		logrus.Infof("deleting cluster role binding [%s]..", clusterRoleBinding)
		if err := deleteClusterRoleBinding(k8sClient, clusterRoleBinding); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	logrus.Infof("Successfully removed ClusterRoleBindings")
	return nil
}
func secretsCleanup(client *kubernetes.Clientset) error {
	logrus.Infof("Starting Secrets cleanup")
	secrets, err := client.CoreV1().Secrets("").List(v1.ListOptions{})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		annotations := cleanupAnnotationsLabels(secret.Annotations)
		labels := cleanupAnnotationsLabels(secret.Labels)
		if len(annotations) != len(secret.Annotations) ||
			len(labels) != len(secret.Labels) {
			if err := retryTo(func() error {
				uSecret, updateErr := client.CoreV1().Secrets(secret.Namespace).Get(secret.Name, v1.GetOptions{})
				if updateErr != nil {
					return updateErr
				}
				uSecret.Annotations = annotations
				uSecret.Labels = labels
				_, updateErr = client.CoreV1().Secrets(uSecret.Namespace).Update(uSecret)
				if updateErr != nil {
					return updateErr
				}
				logrus.Infof("cleaned secret %s/%s", secret.Namespace, secret.Name)
				return nil
			}); err != nil {
				return err
			}
		}
	}
	logrus.Infof("Successfully cleaned up Secrets")
	return nil
}

func namespacesCleanup(client *kubernetes.Clientset) error {
	logrus.Infof("Starting Namespace cleanup")
	nsList, err := client.CoreV1().Namespaces().List(v1.ListOptions{})
	if err != nil {
		return err
	}
	errs := []error{}
	for _, ns := range nsList.Items {
		annotations := cleanupAnnotationsLabels(ns.Annotations)
		labels := cleanupAnnotationsLabels(ns.Labels)
		if len(annotations) != len(ns.Annotations) ||
			len(labels) != len(ns.Labels) {
			if err := retryTo(func() error {
				uns, updateErr := client.CoreV1().Namespaces().Get(ns.Name, v1.GetOptions{})
				if updateErr != nil {
					return updateErr
				}
				uns.Annotations = annotations
				uns.Labels = labels
				if _, updateErr = client.CoreV1().Namespaces().Update(uns); err != nil {
					return updateErr
				}
				logrus.Infof("cleaned namespace %s", ns.Name)
				return nil
			}); err != nil {
				return err
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	logrus.Infof("Successfully cleaned up Namespaces")
	return nil
}

func getGroupAPIResourceList(ctx *cli.Context, apiGroup v1.APIGroup) ([]v1.APIResource, error) {
	dc, err := getDiscoveryClient(ctx)
	if err != nil {
		return nil, err
	}
	srl, err := dc.ServerResourcesForGroupVersion(apiGroup.PreferredVersion.GroupVersion)
	return srl.APIResources, nil
}

func isUpdateble(ar v1.APIResource) bool {
	if strings.Contains(ar.Name, "/") {
		return false
	}
	for _, v := range ar.Verbs {
		if v == "update" {
			return true
		}
	}
	return false
}

func isCattleFinalizers(f []string) bool {
	for _, s := range f {
		if strings.Contains(s, "cattle.io") {
			return true
		}
	}
	return false
}

func removeCattleFinalizers(ctx *cli.Context) error {
	discClient, err := getDiscoveryClient(ctx)
	if err != nil {
		return err
	}

	apiGroupsList, err := discClient.ServerGroups()
	if err != nil {
		return err
	}
	logrus.Infof("Removing Cattle Finalizers")
	for _, apiGroup := range apiGroupsList.Groups {
		groupAPIResources, err := getGroupAPIResourceList(ctx, apiGroup)
		if err != nil {
			return err
		}

		for _, gar := range groupAPIResources {
			logrus.Infof("Checking API resource [%s]", gar.Name)
			if !isUpdateble(gar) {
				continue
			}
			dynClient, err := getGroupDynamicClient(ctx, apiGroup.PreferredVersion.GroupVersion)
			if err != nil {
				return err
			}

			obj, err := dynClient.Resource(&gar, "").List(v1.ListOptions{})
			if err != nil {
				logrus.Warnf("Can't build dynamic client for [%s]: %v\n", gar.Name, err)
				continue
			}
			rList := obj.(*unstructured.UnstructuredList)
			for _, r := range rList.Items {
				if !isCattleFinalizers(r.GetFinalizers()) {
					continue
				}
				if len(r.GetNamespace()) == 0 {
					logrus.Infof("removing %s finalizers", r.GetName())
				} else {
					logrus.Infof("removing %s/%s finalizers", r.GetNamespace(), r.GetName())
				}
				if err := retryTo(func() error {
					ur, updateErr := dynClient.Resource(&gar, r.GetNamespace()).Get(r.GetName(), v1.GetOptions{})
					if updateErr != nil {
						return updateErr
					}
					ur.SetFinalizers(cleanupFinalizers(ur.GetFinalizers()))
					_, updateErr = dynClient.Resource(&gar, ur.GetNamespace()).Update(ur)
					if updateErr != nil {
						return updateErr
					}
					return nil
				}); err != nil {
					return err
				}
			}
		}
	}
	logrus.Infof("Removed all Cattle Finalizers succuessfully")
	return nil
}
