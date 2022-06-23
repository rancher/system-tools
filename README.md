DEPRECATED system-tools
============

>**Note:** System Tools has been deprecated since June 2022.

Rancher 2.0 operations tool kit.

## Commands
### Remove

>**Note:** System Tools has been deprecated since June 2022. The replacement of the Remove command is using the [Rancher Cleanup](https://github.com/rancher/rancher-cleanup) tool.

**Usage**:
```
   system-tools remove [command options] [arguments...]
```

**Options**:

-   `--kubeconfig value, -c value`:                 kubeconfig absolute path [$KUBECONFIG]
-   `--namespace cattle-system, -n cattle-system`:  rancher 2.x deployment namespace. default is cattle-system (default: "cattle-system")
-   `--force`:                                      Skip the the interactive removal confirmation and remove the Rancher deployment right away.


The `system-tools remove` command is used to delete a Rancher 2.x management plane deployment. It operates by applying the following steps:
- Remove Rancher Deployment.
- Remove Rancher-Labeled ClusterRoles and ClusterRoleBindings.
- Remove Labels, Annotations and Finalizers from all resources on the management plane cluster.
- Remove Machines, Clusters, Projects and Users CRDs and corresponding namespaces.
- Remove all resources created under the `management.cattle.io` API group.
- Reamove all CRDs created by Rancher 2.x.
- Remove the Rancher deployment Namespace, default is `cattle-system`.


### Logs

>**Note:** System Tools has been deprecated since June 2022. The replacement of the Logs command is using the [logs-collector](https://github.com/rancherlabs/support-tools/tree/master/collection/rancher/v2.x/logs-collector).

**Usage**:
```
   system-tools logs [command options] [arguments...]
```
**Options**:
-   `--kubeconfig value, -c value`:  managed cluster kubeconfig [$KUBECONFIG]
-   `--output value, -o value`:      cluster logs tarball (default: "cluster-logs.tar")
-   `--node value, -n value`:        fetch logs for a single node

The `system-tools logs` command is used to pull Kubernetes components' Docker container logs deployed by [RKE](https://github.com/rancher/rke) on cluster nodes.

The command works by deploying a DaemonSet on the managed cluster, that uses the Rancher `node-agent` image to mount RKE logs directory and tar the logs on each node and stream them the host running `system-tools`. Once the the logs are pulled, the DaemonSet is removed automatically.

It's also possible to use the `--node` option to pull logs from a specific node.

### Stats

>**Note:** System Tools has been deprecated since June 2022. The replacement of the Stats command is installing/using the `sysstat` package on your nodes (or using a pod), and using the command `/usr/bin/sar -u -r -F 1 1`.

**Usage**:
```
   system-tools stats [command options] [arguments...]
```

**Options**:
-   `--kubeconfig value, -c value`:     managed cluster kubeconfig [$KUBECONFIG]
-   `--node value, -n value`:           show stats for a single node
-   `--stats-command value, -s value`:  alternative command to run on the servers (default: "/usr/bin/sar -u -r -F 1 1")

The `system-tools stats` command is used to pull real-time stats from Rancher-Managed Kubernetes cluster nodes. The default is to pull CPU, memory and disk usage stats.

The command works by deploying a DaemonSet on the managed cluster, that uses the Rancher `node-agent` to run pods used to execute the stats command on each node. Stats are displayed live every 5 seconds. The tool keeps running until the user interrupts its execution using `ctrl+c` which will trigger a cleanup command and remove the stats DaemonSet.

It's also possible to monitor a single node with the `--node` option or run another stats command using the `--stats-command` option.  
## Building

`make`


## Running

`./bin/system-tools`

## License
Copyright (c) 2018-2022 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
