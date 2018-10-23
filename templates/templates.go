package templates

const LogCollectorDSTemplate = `
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: log-collector
  namespace: "cattle-system"
  labels:
    tier: node
    k8s-app: log-collector
spec:
  selector:
    matchLabels:
      tier: node
      k8s-app: log-collector
  template:
    metadata:
      labels:
        tier: node
        k8s-app: log-collector
    spec:
      containers:
      - name: log-collector
        image: {{ .Image }}
        imagePullPolicy: IfNotPresent
        command: ["sh", "-c", "mkdir /tmp/$NODE_NAME;\
        for i in *;\
        do \
         service=$(echo $i | cut -d _ -f 1);\
         log_file=$(readlink $i);\
         cp $log_file /tmp/$NODE_NAME/${service}.log;\
        done; cd /tmp/;\
        tar cvf /tmp/$NODE_NAME.tar $NODE_NAME;\
        sleep 1d"]
        securityContext:
          privileged: true
        volumeMounts:
        - name: logs
          mountPath: /logs
        - name: containers
          mountPath: /var/lib/docker/containers/
        workingDir: /logs
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
      tolerations:
      - key: node-role.kubernetes.io/controlplane
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/etcd
        operator: Exists
        effect: NoExecute
      volumes:
        - name: logs
          hostPath:
            path: /var/lib/rancher/rke/log/
        - name: containers
          hostPath:
            path: /var/lib/docker/containers

`
const StatsDSTemplate = `
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: stats-collector
  namespace: "cattle-system"
  labels:
    tier: node
    k8s-app: stats-collector
spec:
  selector:
    matchLabels:
      tier: node
      k8s-app: stats-collector
  template:
    metadata:
      labels:
        tier: node
        k8s-app: stats-collector
    spec:
      containers:
      - name: stats-collector
        image: {{ .Image }}
        imagePullPolicy: IfNotPresent
        command: ["sh", "-c", "apt update; apt install -y  sysstat;sleep 24h"]
        securityContext:
          privileged: true
      tolerations:
      - key: node-role.kubernetes.io/controlplane
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/etcd
        operator: Exists
        effect: NoExecute
`
