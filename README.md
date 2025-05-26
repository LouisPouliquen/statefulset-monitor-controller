# 🩺 StatefulSet Healer Controller

This controller monitors `StatefulSet` pods for [failure conditions](./internal/controller/helpers.go) and automatically initiates recovery actions when a pod is detected as **unhealthy**.

## 🔍 Description

1. **Watches StatefulSets** referenced by a `StatefulSetHealer` custom resource.
2. **Monitors pod health** based on conditions such as:
   - Not Ready
   - `CrashLoopBackOff`
   - `ImagePullBackOff` / `ErrImagePull`
   - `OOMKilled`
   - Failed init containers
   - Pod in `Failed` or `Unknown` phase
3. If a pod remains in an unhealthy state for too long, the controller **automatically deletes the pod** to trigger a fresh replacement by the StatefulSet controller.

## ⚙️ Recovery logic

The recovery behavior is configured via the following fields in the `StatefulSetHealer` custom resource:

### `targetRef:`

- Specifies the **name of the StatefulSet** to monitor within the same namespace.
- The controller will watch the pods owned by this StatefulSet for health signals.
- Format:
  ```yaml
  targetRef:
    name: <statefulset-name>

### `maxRestartAttempts: <int>`

- Defines the **maximum number of restart attempts** allowed for a pod **before it is considered permanently failed**.
- The controller keeps track of how many times each pod has restarted while in an unhealthy state.

### `failureTimeThreshold: <duration>`

- Specifies the **minimum duration** a pod must remain unhealthy before counting toward `maxRestartAttempts`.
- This avoids reacting too quickly to transient failures.
- For example, a value of `5s` means the pod must continuously remain in a failed state for at least 5 seconds before it's considered a failed attempt.

### Examples

```yaml
apiVersion: monitor.cluster-tools.dev/v1alpha1
kind: StatefulSetHealer
metadata:
  name: statefulsethealer-sample
spec:
  targetRef:
    name: demo-db
  maxRestartAttempts: 3
  failureTimeThreshold: 5s
```

Other examples can be found in : `config/samples`


## Getting Started

### Prerequisites
- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/stshealer:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/stshealer:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/stshealer:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/stshealer/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

