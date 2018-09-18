system-tools
============

Rancher 2.0 operations tool kit.

#### Commands:
- **Remove**

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


## Building

`make`


## Running

`./bin/system-tools`

## License
Copyright (c) 2018 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
