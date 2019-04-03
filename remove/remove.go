package remove

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/rancher/system-tools/clients"
	"github.com/rancher/system-tools/utils"
	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/rancher/types/config"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

const (
	CattleControllerName = "controller.cattle.io"
	CattleLabelBase      = "cattle.io"
	DefaultRetryCount    = 3
)

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
var ForceFlag cli.Flag = cli.BoolFlag{
	Name:  "force",
	Usage: "Force removal of the cluster",
}

func DoRemoveRancher(ctx *cli.Context) error {
	cattleNamespace := ctx.String("namespace")
	force := ctx.Bool("force")
	if !force {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Are you sure you want to remove Rancher Management Plane in Namespace [%s] [y/n]: ", cattleNamespace)
		input, err := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if err != nil {
			return err
		}
		if input != "y" && input != "Y" {
			return nil
		}
	}
	logrus.Infof("Removing Rancher management plane in namespace: [%s]", cattleNamespace)
	// setup
	logrus.Infof("Getting connection configuration")
	restConfig, err := clients.GetRestConfig(ctx)
	if err != nil {
		return err
	}
	management, err := config.NewManagementContext(*restConfig)
	if err != nil {
		return err
	}
	k8sClient, err := clients.GetClientSet(ctx)
	if err != nil {
		return err
	}
	if err := utils.RetryWithCount(func() error {
		return removeCattleDeployment(ctx)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return clusterRoleBindginsCleanup(k8sClient)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return clusterRolesCleanup(k8sClient)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return removeCattleAnnotationsFinalizersLabels(ctx)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return projectsCleanup(management, k8sClient)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return nodesCleanup(management)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return clustersCleanup(management, k8sClient)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return usersCleanup(management, k8sClient)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return removeCattleAPIGroupResources(ctx)
	}, DefaultRetryCount); err != nil {
		return err
	}

	if err := utils.RetryWithCount(func() error {
		return removeCattleCRDs(ctx)
	}, DefaultRetryCount); err != nil {
		return err
	}
	// final cleanup
	logrus.Infof("Removing Rancher Namespace [%s]", cattleNamespace)
	if err := utils.RetryWithCount(func() error {
		return deleteNamespace(k8sClient, cattleNamespace)
	}, DefaultRetryCount); err != nil && !errors.IsNotFound(err) {
		return err
	}
	logrus.Infof("Successfully removed namespace [%s]", cattleNamespace)
	logrus.Infof("Rancher Management Plane removed successfully")
	return nil
}

func removeCattleDeployment(ctx *cli.Context) error {
	logrus.Infof("Removing Cattle deployment")
	clientSet, err := clients.GetClientSet(ctx)
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
	return utils.RetryTo(func() error {
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

func getGroupAPIResourceList(ctx *cli.Context, apiGroup v1.APIGroup) ([]v1.APIResource, error) {
	dc, err := clients.GetDiscoveryClient(ctx)
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

func hasCattleMark(resource unstructured.Unstructured) bool {
	for _, s := range resource.GetFinalizers() {
		if strings.Contains(s, CattleLabelBase) {
			return true
		}
	}
	for k := range resource.GetLabels() {
		if strings.Contains(k, CattleLabelBase) && !strings.Contains(k, "cattle.io/creator") {
			return true
		}
	}
	for k := range resource.GetAnnotations() {
		if strings.Contains(k, CattleLabelBase) {
			return true
		}
	}
	return false
}

func removeCattleMark(resource *unstructured.Unstructured) {
	resource.SetFinalizers(cleanupFinalizers(resource.GetFinalizers()))
	resource.SetLabels(cleanupAnnotationsLabels(resource.GetLabels()))
	resource.SetAnnotations(cleanupAnnotationsLabels(resource.GetAnnotations()))
}
func removeCattleAnnotationsFinalizersLabels(ctx *cli.Context) error {
	discClient, err := clients.GetDiscoveryClient(ctx)
	if err != nil {
		return err
	}

	apiGroupsList, err := discClient.ServerGroups()
	if err != nil {
		return err
	}
	logrus.Infof("Removing Cattle Annotations, Finalizers and Labels")
	for _, apiGroup := range apiGroupsList.Groups {
		groupAPIResources, err := getGroupAPIResourceList(ctx, apiGroup)
		if err != nil {
			return err
		}

		for _, gar := range groupAPIResources {
			grv := schema.GroupVersionResource{
				Group:    gar.Group,
				Version:  gar.Version,
				Resource: gar.Name,
			}
			logrus.Infof("Checking API resource [%s]", gar.Name)
			if !isUpdateble(gar) {
				continue
			}
			dynClient, err := clients.GetGroupDynamicClient(ctx, apiGroup.PreferredVersion.GroupVersion)
			if err != nil {
				return err
			}

			rList, err := dynClient.Resource(grv).List(v1.ListOptions{})
			if err != nil {
				logrus.Warnf("Can't build dynamic client for [%s]: %v\n", gar.Name, err)
				continue
			}

			for _, r := range rList.Items {
				if !hasCattleMark(r) {
					continue
				}
				if len(r.GetNamespace()) == 0 {
					logrus.Infof("cleaning %s", r.GetName())
				} else {
					logrus.Infof("cleaning %s/%s", r.GetNamespace(), r.GetName())
				}
				if err := utils.RetryTo(func() error {
					ur, updateErr := dynClient.Resource(grv).Get(r.GetName(), v1.GetOptions{})
					if updateErr != nil {
						return updateErr
					}
					removeCattleMark(ur)
					_, updateErr = dynClient.Resource(grv).Update(ur, v1.UpdateOptions{})
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
	logrus.Infof("Removed all Cattle Annotations, Finalizers and Labels successfully")
	return nil
}

func removeCattleAPIGroupResources(ctx *cli.Context) error {
	discClient, err := clients.GetDiscoveryClient(ctx)
	if err != nil {
		return err
	}

	apiGroupsList, err := discClient.ServerGroups()
	if err != nil {
		return err
	}
	logrus.Infof("Removing Cattle resources")
	for _, apiGroup := range apiGroupsList.Groups {
		if !strings.Contains(apiGroup.Name, CattleLabelBase) {
			continue
		}
		dynClient, err := clients.GetGroupDynamicClient(ctx, apiGroup.PreferredVersion.GroupVersion)
		if err != nil {
			return err
		}
		apiResourceList, err := discClient.ServerResourcesForGroupVersion(apiGroup.PreferredVersion.GroupVersion)
		if err != nil {
			return err
		}
		for _, apiResource := range apiResourceList.APIResources {
			grv := schema.GroupVersionResource{
				Group:    apiResource.Group,
				Version:  apiResource.Version,
				Resource: apiResource.Name,
			}
			resourcesList, err := dynClient.Resource(grv).List(v1.ListOptions{})
			if err != nil {
				logrus.Warnf("Can't build dynamic client for [%s]: %v\n", apiResource.Name, err)
				continue
			}
			for _, resource := range resourcesList.Items {
				if len(resource.GetNamespace()) == 0 {
					logrus.Infof("removing %s", resource.GetName())
				} else {
					logrus.Infof("removing %s/%s", resource.GetNamespace(), resource.GetName())
				}
				if err := dynClient.Resource(grv).Delete(resource.GetName(), &v1.DeleteOptions{}); err != nil {
					return err
				}
			}
		}

	}
	logrus.Infof("Removed all Cattle resources succuessfully")
	return nil
}

func removeCattleCRDs(ctx *cli.Context) error {
	apiExtClient, err := clients.GetAPIExtensionsClient(ctx)
	if err != nil {
		return err
	}
	crdList, err := apiExtClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(v1.ListOptions{})
	if err != nil {
		return err
	}
	for _, crd := range crdList.Items {
		if strings.Contains(crd.Name, CattleLabelBase) {
			if err := apiExtClient.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crd.Name, &v1.DeleteOptions{}); err != nil {
				return err
			}
		}
	}
	logrus.Infof("Removed all Cattle CRDs succuessfully")
	return nil
}
