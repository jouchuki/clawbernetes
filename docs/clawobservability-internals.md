# ClawObservability CRD -- Technical Documentation

This document provides a comprehensive reference for the `ClawObservability` custom resource
in the Clawbernetes operator. It covers the CRD surface area, how agents consume it, and
the full Kubernetes internals of its reconciliation loop.

---

## Section 1: Technical Summary

### What ClawObservability Is

`ClawObservability` declares the telemetry stack -- Grafana Tempo (for distributed tracing)
and Grafana (for visualization) -- as a first-class Kubernetes primitive. Instead of manually
deploying and wiring an observability stack, a single CR tells the operator to stand up the
entire pipeline: an OTLP collector (Tempo), a visualization layer (Grafana with pre-provisioned
datasources and dashboards), and the plumbing that connects every agent pod to the collector.

CRD metadata:

| Field | Value |
|---|---|
| Group | `claw.clawbernetes.io` |
| Version | `v1` |
| Kind | `ClawObservability` |
| Scope | `Namespaced` |
| Plural | `clawobservabilities` |

Defined in: `api/v1/clawobservability_types.go`

### Spec Fields

#### `spec.tempo`

Controls the Grafana Tempo OTLP collector.

| Field | Type | Description |
|---|---|---|
| `tempo.enabled` | `bool` | When `true`, the operator deploys a Tempo Deployment, Service, and ConfigMap. |
| `tempo.retentionDays` | `int` | How many days to retain trace data. Declared on the spec but not yet wired into the Tempo config YAML (the Tempo config uses local backend defaults). |
| `tempo.storage.size` | `string` | PVC size for trace storage (e.g. `"10Gi"`). Declared on the spec for future PVC provisioning. |
| `tempo.storage.storageClass` | `string` | Kubernetes StorageClass name. Empty string uses the cluster default. |

Go type (`api/v1/clawobservability_types.go:24-47`):

```go
type TempoStorageSpec struct {
    Size         string `json:"size,omitempty"`
    StorageClass string `json:"storageClass,omitempty"`
}

type TempoSpec struct {
    Enabled       bool             `json:"enabled,omitempty"`
    RetentionDays int              `json:"retentionDays,omitempty"`
    Storage       TempoStorageSpec `json:"storage,omitempty"`
}
```

#### `spec.grafana`

Controls the Grafana visualization layer.

| Field | Type | Description |
|---|---|---|
| `grafana.enabled` | `bool` | When `true`, the operator deploys Grafana with provisioned datasources and dashboards. |
| `grafana.dashboards` | `[]string` | List of pre-built dashboard names to install (e.g. `["fleet-overview"]`). |
| `grafana.expose` | `string` | How Grafana is exposed outside the cluster. Enum: `"port-forward"`, `"loadbalancer"`, `"ingress"`. |
| `grafana.adminCredentialsSecret` | `string` | Name of a Secret containing Grafana admin credentials. Currently the operator deploys Grafana with anonymous auth enabled, so this field is reserved for future use. |

Go type (`api/v1/clawobservability_types.go:49-68`):

```go
type GrafanaSpec struct {
    Enabled                bool     `json:"enabled,omitempty"`
    Dashboards             []string `json:"dashboards,omitempty"`
    AdminCredentialsSecret string   `json:"adminCredentialsSecret,omitempty"`
    Expose                 string   `json:"expose,omitempty"`
}
```

The `Expose` field carries a kubebuilder validation enum:

```go
// +kubebuilder:validation:Enum=port-forward;loadbalancer;ingress
Expose string `json:"expose,omitempty"`
```

#### `spec.otlpEndpoint`

Type: `string`

The cluster-internal OTLP endpoint that agent pods write traces to. This is the value that
gets injected as `OTEL_EXPORTER_OTLP_ENDPOINT` into every agent container that references
this observability resource. Typical value:

```
http://tempo.<namespace>.svc.cluster.local:4318
```

Port 4318 is the OTLP/HTTP receiver on Tempo.

#### `spec.otlpProtocol`

Type: `string`

The OTLP transport protocol, typically `"http/protobuf"`. Used in the `diagnostics-otel`
extension configuration inside `openclaw.json`.

### Full YAML Example

```yaml
# ClawObservability declares the full telemetry stack.
# The operator will deploy Tempo + Grafana and wire all
# referencing agents to push traces via OTLP.
apiVersion: claw.clawbernetes.io/v1
kind: ClawObservability
metadata:
  name: fleet-observability          # Referenced by ClawAgent.spec.observability
  namespace: clawbernetes
spec:
  tempo:
    enabled: true                    # Deploy a Tempo instance in this namespace
    retentionDays: 7                 # Retain traces for 7 days
    storage:
      size: 10Gi                     # PVC size for /var/tempo
      storageClass: ""               # Empty = cluster default StorageClass
  grafana:
    enabled: true                    # Deploy Grafana alongside Tempo
    dashboards:
      - fleet-overview               # Install the built-in fleet overview dashboard
    expose: port-forward             # Access via kubectl port-forward
  otlpEndpoint: "http://tempo.clawbernetes.svc.cluster.local:4318"
  otlpProtocol: http/protobuf       # OTLP transport protocol
```

Sample file: `config/samples/claw_v1_clawobservability.yaml`

### How Agents Reference ClawObservability

A `ClawAgent` resource points to a `ClawObservability` by name via `spec.observability`:

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: my-agent
  namespace: clawbernetes
spec:
  observability: fleet-observability   # <-- name of the ClawObservability CR
  telemetryCapture:
    inputMessages: true
    outputMessages: true
    systemInstructions: false
    toolDefinitions: true
    toolContent: true
    sampleRate: "1.0"
  # ...
```

Go type for the reference (`api/v1/clawagent_types.go:213-219`):

```go
// observability references the ClawObservability resource name.
Observability string `json:"observability,omitempty"`

// telemetryCapture controls what content is captured in OTEL spans.
TelemetryCapture TelemetryCaptureSpec `json:"telemetryCapture,omitempty"`
```

The `TelemetryCaptureSpec` (`api/v1/clawagent_types.go:39-64`):

```go
type TelemetryCaptureSpec struct {
    InputMessages      bool   `json:"inputMessages,omitempty"`
    OutputMessages     bool   `json:"outputMessages,omitempty"`
    SystemInstructions bool   `json:"systemInstructions,omitempty"`
    ToolDefinitions    bool   `json:"toolDefinitions,omitempty"`
    ToolContent        bool   `json:"toolContent,omitempty"`
    SampleRate         string `json:"sampleRate,omitempty"`
}
```

### OTLP Injection -- Agent Controller Code

When reconciling a `ClawAgent`, the agent controller resolves the OTLP endpoint from the
referenced `ClawObservability` resource.

**Step 1: Resolve the endpoint** (`internal/controller/clawagent_controller.go:82-95`):

```go
// --- Resolve the OTLP endpoint from the referenced ClawObservability ---
otlpEndpoint := ""
if agent.Spec.Observability != "" {
    obs := &clawv1.ClawObservability{}
    obsKey := types.NamespacedName{Name: agent.Spec.Observability, Namespace: ns}
    if err := r.Get(ctx, obsKey, obs); err != nil {
        if !apierrors.IsNotFound(err) {
            return ctrl.Result{}, err
        }
        log.Info("referenced ClawObservability not found, skipping OTEL injection",
            "name", agent.Spec.Observability)
    } else {
        otlpEndpoint = obs.Spec.OTLPEndpoint
    }
}
```

**Step 2: Inject env vars into the agent pod** (`internal/controller/clawagent_controller.go:702-711`):

```go
env := []corev1.EnvVar{
    {Name: "OTEL_SERVICE_NAME", Value: name},
}
if otlpEndpoint != "" {
    env = append(env,
        corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: otlpEndpoint},
        corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_HEADERS", Value: ""},
    )
}
```

**Step 3: Configure diagnostics-otel in openclaw.json** (`internal/controller/clawagent_controller.go:1033-1075`):

```go
// --- diagnostics-otel: built-in extension in the orq-ai/openclaw fork ---
if otlpEndpoint != "" {
    otelCfg := map[string]any{
        "enabled":     true,
        "endpoint":    otlpEndpoint,
        "protocol":    "http/protobuf",
        "serviceName": name,
        "traces":      true,
        "metrics":     true,
        "logs":        true,
        "sampleRate":  1.0,
    }

    tc := agent.Spec.TelemetryCapture
    captureContent := map[string]any{
        "inputMessages":      true,
        "outputMessages":     true,
        "systemInstructions": true,
        "toolDefinitions":    true,
        "toolContent":        true,
    }
    if tc.InputMessages || tc.OutputMessages || tc.SystemInstructions ||
        tc.ToolDefinitions || tc.ToolContent {
        captureContent["inputMessages"] = tc.InputMessages
        captureContent["outputMessages"] = tc.OutputMessages
        captureContent["systemInstructions"] = tc.SystemInstructions
        captureContent["toolDefinitions"] = tc.ToolDefinitions
        captureContent["toolContent"] = tc.ToolContent
    }
    otelCfg["captureContent"] = captureContent

    if tc.SampleRate != "" {
        if sr, err := strconv.ParseFloat(tc.SampleRate, 64); err == nil {
            otelCfg["sampleRate"] = sr
        }
    }

    cfg["diagnostics"] = map[string]any{
        "enabled": true,
        "otel":    otelCfg,
    }
}
```

**Step 4: Enable the diagnostics-otel plugin** (`internal/controller/clawagent_controller.go:1166-1171`):

```go
if otlpEndpoint != "" {
    pluginEntries["diagnostics-otel"] = map[string]any{
        "enabled": true,
    }
}
```

---

## Section 2: Kubernetes Internals Deep Dive

### The ClawObservabilityReconciler

The observability stack has its own dedicated reconciler, separate from the agent controller.
It is registered in the manager via `SetupWithManager`.

Struct definition (`internal/controller/clawobservability_controller.go:42-45`):

```go
type ClawObservabilityReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}
```

Manager registration (`internal/controller/clawobservability_controller.go:408-416`):

```go
func (r *ClawObservabilityReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&clawv1.ClawObservability{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        Owns(&corev1.ConfigMap{}).
        Named("clawobservability").
        Complete(r)
}
```

The controller watches `ClawObservability` as the primary resource and **owns** Deployments,
Services, and ConfigMaps. This means that if any of those child resources are deleted or
modified externally, the controller-runtime framework will re-enqueue the parent
`ClawObservability` for reconciliation.

### Reconcile Loop

The main `Reconcile` function (`internal/controller/clawobservability_controller.go:55-115`)
follows this flow:

1. Fetch the `ClawObservability` CR (return early if deleted).
2. If `spec.tempo.enabled` is true, ensure Tempo resources exist (ConfigMap, Deployment, Service).
3. If `spec.grafana.enabled` is true, ensure Grafana resources exist (3 ConfigMaps, Deployment, Service).
4. Check deployment readiness for both Tempo and Grafana.
5. Update the CR status with `tempoReady` and `grafanaReady` booleans.

```go
func (r *ClawObservabilityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)

    obs := &clawv1.ClawObservability{}
    if err := r.Get(ctx, req.NamespacedName, obs); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    ns := obs.Namespace

    // --- Tempo ---
    if obs.Spec.Tempo.Enabled {
        if err := r.ensureResource(ctx, obs, r.tempoConfigMap(ns), "tempo-config-cm"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.tempoDeployment(obs, ns), "tempo-deployment"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.tempoService(ns), "tempo-service"); err != nil {
            return ctrl.Result{}, err
        }
    }

    // --- Grafana ---
    if obs.Spec.Grafana.Enabled {
        if err := r.ensureResource(ctx, obs, r.grafanaDatasourceConfigMap(obs, ns), "grafana-datasource-cm"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.grafanaDashboardConfigMap(ns), "grafana-dashboard-cm"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.grafanaDashboardJSONConfigMap(ns), "grafana-dashboard-json-cm"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.grafanaDeployment(ns), "grafana-deployment"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, obs, r.grafanaService(ns), "grafana-service"); err != nil {
            return ctrl.Result{}, err
        }
    }

    // --- Status ---
    tempoReady := r.isDeploymentAvailable(ctx, ns, "tempo")
    grafanaReady := r.isDeploymentAvailable(ctx, ns, "grafana")

    obs.Status.TempoReady = tempoReady
    obs.Status.GrafanaReady = grafanaReady

    if err := r.Status().Update(ctx, obs); err != nil {
        log.Error(err, "unable to update ClawObservability status")
        return ctrl.Result{}, err
    }

    log.Info("reconciled ClawObservability", "tempo", tempoReady, "grafana", grafanaReady)
    return ctrl.Result{}, nil
}
```

### What the Reconciler Deploys

When both Tempo and Grafana are enabled, the reconciler creates **8 Kubernetes resources**:

#### Tempo Resources

**1. ConfigMap `tempo-config`** -- contains the full `tempo.yaml` configuration.

(`internal/controller/clawobservability_controller.go:155-183`)

```go
func (r *ClawObservabilityReconciler) tempoConfigMap(ns string) *corev1.ConfigMap {
    tempoYAML := `stream_over_http_enabled: true
server:
  http_listen_port: 3200
distributor:
  receivers:
    otlp:
      protocols:
        http:
          endpoint: "0.0.0.0:4318"
storage:
  trace:
    backend: local
    local:
      path: /var/tempo/traces
    wal:
      path: /var/tempo/wal
`
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "tempo-config",
            Namespace: ns,
            Labels:    map[string]string{"app": "tempo", "app.kubernetes.io/managed-by": "clawbernetes"},
        },
        Data: map[string]string{
            "tempo.yaml": tempoYAML,
        },
    }
}
```

Key Tempo config details:
- OTLP HTTP receiver listens on `0.0.0.0:4318` (the port agents push to).
- Tempo HTTP API listens on port `3200` (the port Grafana queries).
- Storage backend is `local` with traces at `/var/tempo/traces` and WAL at `/var/tempo/wal`.
- `stream_over_http_enabled: true` allows streaming search results via the HTTP API.

**2. Deployment `tempo`** -- single-replica Tempo pod.

(`internal/controller/clawobservability_controller.go:185-233`)

```go
func (r *ClawObservabilityReconciler) tempoDeployment(obs *clawv1.ClawObservability, ns string) *appsv1.Deployment {
    labels := map[string]string{"app": "tempo", "app.kubernetes.io/managed-by": "clawbernetes"}
    replicas := int32(1)

    return &appsv1.Deployment{
        // ...
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{MatchLabels: labels},
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "tempo",
                        Image: "grafana/tempo:2.6.1",
                        Args:  []string{"-config.file=/etc/tempo/tempo.yaml"},
                        Ports: []corev1.ContainerPort{
                            {Name: "otlp-http", ContainerPort: 4318},
                            {Name: "tempo-http", ContainerPort: 3200},
                        },
                        VolumeMounts: []corev1.VolumeMount{
                            {Name: "tempo-config", MountPath: "/etc/tempo", ReadOnly: true},
                        },
                    }},
                    Volumes: []corev1.Volume{{
                        Name: "tempo-config",
                        VolumeSource: corev1.VolumeSource{
                            ConfigMap: &corev1.ConfigMapVolumeSource{
                                LocalObjectReference: corev1.LocalObjectReference{
                                    Name: "tempo-config",
                                },
                            },
                        },
                    }},
                },
            },
        },
    }
}
```

Image: `grafana/tempo:2.6.1`. Two ports exposed: `4318` (OTLP HTTP write path) and `3200`
(Tempo query API). The `tempo-config` ConfigMap is mounted at `/etc/tempo`.

**3. Service `tempo`** -- exposes both ports cluster-internally.

(`internal/controller/clawobservability_controller.go:235-251`)

```go
func (r *ClawObservabilityReconciler) tempoService(ns string) *corev1.Service {
    labels := map[string]string{"app": "tempo", "app.kubernetes.io/managed-by": "clawbernetes"}
    return &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{Name: "tempo", Namespace: ns, Labels: labels},
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports: []corev1.ServicePort{
                {Name: "otlp-http", Port: 4318, TargetPort: intstr.FromInt(4318)},
                {Name: "tempo-http", Port: 3200, TargetPort: intstr.FromInt(3200)},
            },
        },
    }
}
```

This Service is what makes `http://tempo.<namespace>.svc.cluster.local:4318` resolvable
from agent pods.

#### Grafana Resources

**4. ConfigMap `grafana-datasources`** -- auto-provisions the Tempo datasource.

(`internal/controller/clawobservability_controller.go:257-283`)

```go
func (r *ClawObservabilityReconciler) grafanaDatasourceConfigMap(
    obs *clawv1.ClawObservability, ns string,
) *corev1.ConfigMap {
    endpoint := fmt.Sprintf("http://tempo.%s.svc.cluster.local:3200", ns)
    datasourceYAML := fmt.Sprintf(`apiVersion: 1
datasources:
  - name: Tempo
    type: tempo
    uid: tempo
    access: proxy
    url: %s
    isDefault: true
    editable: false
`, endpoint)
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name: "grafana-datasources", Namespace: ns,
            Labels: map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"},
        },
        Data: map[string]string{"datasources.yaml": datasourceYAML},
    }
}
```

Note: Grafana queries Tempo on port **3200** (the HTTP query API), not port 4318 (the OTLP
write port). The datasource UID is `"tempo"`, which is referenced by the embedded dashboard
panels.

**5. ConfigMap `grafana-dashboards`** -- dashboard provisioning provider config.

(`internal/controller/clawobservability_controller.go:285-308`)

```go
func (r *ClawObservabilityReconciler) grafanaDashboardConfigMap(ns string) *corev1.ConfigMap {
    providerYAML := `apiVersion: 1
providers:
  - name: clawbernetes
    orgId: 1
    folder: Clawbernetes
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
`
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name: "grafana-dashboards", Namespace: ns,
            Labels: map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"},
        },
        Data: map[string]string{"dashboards.yaml": providerYAML},
    }
}
```

This tells Grafana to load any `.json` dashboard files from `/var/lib/grafana/dashboards`
and place them in a folder called "Clawbernetes".

**6. ConfigMap `grafana-dashboard-json`** -- contains the embedded fleet overview dashboard.

(`internal/controller/clawobservability_controller.go:310-321`)

```go
func (r *ClawObservabilityReconciler) grafanaDashboardJSONConfigMap(ns string) *corev1.ConfigMap {
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name: "grafana-dashboard-json", Namespace: ns,
            Labels: map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"},
        },
        Data: map[string]string{
            "fleet-overview.json": fleetOverviewDashboardJSON,
        },
    }
}
```

The `fleetOverviewDashboardJSON` variable comes from `go:embed` (see below).

**7. Deployment `grafana`** -- single-replica Grafana pod.

(`internal/controller/clawobservability_controller.go:323-388`)

```go
func (r *ClawObservabilityReconciler) grafanaDeployment(ns string) *appsv1.Deployment {
    labels := map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"}
    replicas := int32(1)

    return &appsv1.Deployment{
        // ...
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "grafana",
                        Image: "grafana/grafana:latest",
                        Ports: []corev1.ContainerPort{
                            {Name: "http", ContainerPort: 3000},
                        },
                        Env: []corev1.EnvVar{
                            {Name: "GF_AUTH_ANONYMOUS_ENABLED", Value: "true"},
                            {Name: "GF_AUTH_ANONYMOUS_ORG_ROLE", Value: "Admin"},
                            {Name: "GF_AUTH_DISABLE_LOGIN_FORM", Value: "true"},
                        },
                        VolumeMounts: []corev1.VolumeMount{
                            {Name: "datasources",
                             MountPath: "/etc/grafana/provisioning/datasources"},
                            {Name: "dashboard-provider",
                             MountPath: "/etc/grafana/provisioning/dashboards"},
                            {Name: "dashboard-json",
                             MountPath: "/var/lib/grafana/dashboards"},
                        },
                    }},
                    Volumes: []corev1.Volume{
                        {Name: "datasources", VolumeSource: corev1.VolumeSource{
                            ConfigMap: &corev1.ConfigMapVolumeSource{
                                LocalObjectReference: corev1.LocalObjectReference{
                                    Name: "grafana-datasources"},
                            },
                        }},
                        {Name: "dashboard-provider", VolumeSource: corev1.VolumeSource{
                            ConfigMap: &corev1.ConfigMapVolumeSource{
                                LocalObjectReference: corev1.LocalObjectReference{
                                    Name: "grafana-dashboards"},
                            },
                        }},
                        {Name: "dashboard-json", VolumeSource: corev1.VolumeSource{
                            ConfigMap: &corev1.ConfigMapVolumeSource{
                                LocalObjectReference: corev1.LocalObjectReference{
                                    Name: "grafana-dashboard-json"},
                            },
                        }},
                    },
                },
            },
        },
    }
}
```

Three volumes are mounted, mapping ConfigMaps to Grafana's provisioning directories:

| Volume | ConfigMap | Mount Path | Purpose |
|---|---|---|---|
| `datasources` | `grafana-datasources` | `/etc/grafana/provisioning/datasources` | Auto-configures the Tempo datasource |
| `dashboard-provider` | `grafana-dashboards` | `/etc/grafana/provisioning/dashboards` | Tells Grafana where to find dashboard JSON |
| `dashboard-json` | `grafana-dashboard-json` | `/var/lib/grafana/dashboards` | Contains the actual `fleet-overview.json` |

Grafana env vars disable the login form and grant anonymous users Admin access. This is a
development-friendly default; the `adminCredentialsSecret` field exists for production use.

**8. Service `grafana`** -- exposes port 3000.

(`internal/controller/clawobservability_controller.go:390-405`)

```go
func (r *ClawObservabilityReconciler) grafanaService(ns string) *corev1.Service {
    labels := map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"}
    return &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{Name: "grafana", Namespace: ns, Labels: labels},
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports: []corev1.ServicePort{
                {Name: "http", Port: 3000, TargetPort: intstr.FromInt(3000)},
            },
        },
    }
}
```

### How the Fleet Overview Dashboard JSON Is Embedded via go:embed

The dashboard JSON lives at `internal/controller/fleet-overview-dashboard.json` (472 lines).
It is compiled into the operator binary at build time using Go's `embed` package.

(`internal/controller/clawobservability_controller.go:21,38-39`):

```go
import (
    _ "embed"
    // ...
)

//go:embed fleet-overview-dashboard.json
var fleetOverviewDashboardJSON string
```

The `//go:embed` directive tells the Go compiler to read
`internal/controller/fleet-overview-dashboard.json` at build time and store its contents in
the `fleetOverviewDashboardJSON` package-level string variable. At runtime, the reconciler
writes this string into the `grafana-dashboard-json` ConfigMap (key: `"fleet-overview.json"`),
which Grafana picks up through its file-based provisioning.

The dashboard itself (titled "Clawbernetes Fleet Overview") uses the Tempo datasource
(uid: `"tempo"`) and includes panels for Total LLM Calls, agent-level trace exploration, and
fleet-wide monitoring. It auto-refreshes every 30 seconds.

### How Agents Consume the OTLP Endpoint

The flow from ClawObservability to a running agent pod involves two controllers:

1. **ClawObservabilityReconciler** deploys Tempo and Grafana, making the OTLP endpoint
   (`tempo.<ns>.svc.cluster.local:4318`) available as a cluster-internal Service.

2. **ClawAgentReconciler** reads the referenced ClawObservability, extracts `spec.otlpEndpoint`,
   and injects it in two places:

   **a) Container environment variables** -- standard OTEL SDK env vars so any
   OpenTelemetry-instrumented code in the agent picks up the endpoint automatically:

   ```
   OTEL_SERVICE_NAME=<agent-name>
   OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo.clawbernetes.svc.cluster.local:4318
   OTEL_EXPORTER_OTLP_HEADERS=
   ```

   **b) The `openclaw.json` config file** -- under the `diagnostics` key, which configures the
   `diagnostics-otel` built-in extension in OpenClaw.

### How telemetryCapture Fields Control Span Content

The `TelemetryCaptureSpec` on the ClawAgent governs what data the `diagnostics-otel` extension
includes in OTEL spans. The logic is in `buildOpenclawConfig`
(`internal/controller/clawagent_controller.go:1046-1069`).

**Default behavior:** When no `telemetryCapture` fields are explicitly set (all booleans are
`false`, which is Go's zero value), the controller defaults everything to `true`:

```go
captureContent := map[string]any{
    "inputMessages":      true,
    "outputMessages":     true,
    "systemInstructions": true,
    "toolDefinitions":    true,
    "toolContent":        true,
}
```

**Override behavior:** If any field on the spec is `true`, the controller uses the spec values
directly:

```go
if tc.InputMessages || tc.OutputMessages || tc.SystemInstructions ||
    tc.ToolDefinitions || tc.ToolContent {
    captureContent["inputMessages"] = tc.InputMessages
    captureContent["outputMessages"] = tc.OutputMessages
    captureContent["systemInstructions"] = tc.SystemInstructions
    captureContent["toolDefinitions"] = tc.ToolDefinitions
    captureContent["toolContent"] = tc.ToolContent
}
```

This means: to selectively disable a capture field, you must explicitly set at least one field
to `true`. For example, to capture only input/output messages but not tool content:

```yaml
telemetryCapture:
  inputMessages: true
  outputMessages: true
  systemInstructions: false
  toolDefinitions: false
  toolContent: false
```

**Sample rate** is handled separately -- parsed from string to float64:

```go
if tc.SampleRate != "" {
    if sr, err := strconv.ParseFloat(tc.SampleRate, 64); err == nil {
        otelCfg["sampleRate"] = sr
    }
}
```

| Field | What It Captures | Default |
|---|---|---|
| `inputMessages` | LLM input messages (user prompts) in span attributes | `true` |
| `outputMessages` | LLM output messages (assistant responses) in span attributes | `true` |
| `systemInstructions` | System prompt / instructions in span attributes | `true` |
| `toolDefinitions` | Tool JSON schemas in span attributes | `true` |
| `toolContent` | Actual tool call arguments and results in span attributes | `true` |
| `sampleRate` | Fraction of traces to sample (0.0 to 1.0) | `1.0` |

### The diagnostics-otel Extension Configuration in openclaw.json

The generated `openclaw.json` has two relevant sections when OTLP is configured:

**1. Top-level `diagnostics` block** (controls the extension's runtime behavior):

```json
{
  "diagnostics": {
    "enabled": true,
    "otel": {
      "enabled": true,
      "endpoint": "http://tempo.clawbernetes.svc.cluster.local:4318",
      "protocol": "http/protobuf",
      "serviceName": "<agent-name>",
      "traces": true,
      "metrics": true,
      "logs": true,
      "sampleRate": 1.0,
      "captureContent": {
        "inputMessages": true,
        "outputMessages": true,
        "systemInstructions": true,
        "toolDefinitions": true,
        "toolContent": true
      }
    }
  }
}
```

**2. Plugin entry** (enables the extension in OpenClaw's plugin system):

```json
{
  "plugins": {
    "diagnostics-otel": {
      "enabled": true
    }
  }
}
```

Code reference for the plugin entry (`internal/controller/clawagent_controller.go:1166-1171`):

```go
if otlpEndpoint != "" {
    pluginEntries["diagnostics-otel"] = map[string]any{
        "enabled": true,
    }
}
```

### Status Fields: tempoReady and grafanaReady

The reconciler determines readiness by checking whether the underlying Deployment has at least
one available replica.

(`internal/controller/clawobservability_controller.go:138-149`):

```go
func (r *ClawObservabilityReconciler) isDeploymentAvailable(ctx context.Context, ns, name string) bool {
    dep := &appsv1.Deployment{}
    if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, dep); err != nil {
        return false
    }
    for _, c := range dep.Status.Conditions {
        if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
            return true
        }
    }
    return false
}
```

The function looks for the standard Kubernetes Deployment condition `Available` with status
`True`. If the Deployment does not exist or has no available replicas, it returns `false`.

These booleans are written to the CR status and surfaced as printer columns in `kubectl get`:

```
$ kubectl get clawobservabilities
NAME                  TEMPO   GRAFANA
fleet-observability   true    true
```

CRD printer column definitions (from the CRD YAML):

```yaml
additionalPrinterColumns:
  - jsonPath: .status.tempoReady
    name: Tempo
    type: boolean
  - jsonPath: .status.grafanaReady
    name: Grafana
    type: boolean
```

### Grafana Expose Modes

The `spec.grafana.expose` field accepts three values:

| Value | Behavior |
|---|---|
| `port-forward` | The operator creates a ClusterIP Service. Users access Grafana via `kubectl port-forward svc/grafana 3000:3000`. This is the default/recommended mode for development. |
| `loadbalancer` | Intended to create a LoadBalancer-type Service for cloud environments. Currently the Service is always created as ClusterIP -- this mode is defined in the enum for future implementation. |
| `ingress` | Intended to create an Ingress resource. Also reserved for future implementation in the enum. |

Currently, the `grafanaService` function always creates a ClusterIP Service regardless of
the `expose` value. The enum validation ensures only valid values can be set on the CR.

### The ensureResource Pattern

All child resources use a create-if-not-exists pattern
(`internal/controller/clawobservability_controller.go:118-135`):

```go
func (r *ClawObservabilityReconciler) ensureResource(
    ctx context.Context,
    owner *clawv1.ClawObservability,
    obj client.Object,
    desc string,
) error {
    log := logf.FromContext(ctx)

    if err := ctrl.SetControllerReference(owner, obj, r.Scheme); err != nil {
        return fmt.Errorf("setting owner reference on %s: %w", desc, err)
    }

    key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
    existing := obj.DeepCopyObject().(client.Object)
    if err := r.Get(ctx, key, existing); err != nil {
        if apierrors.IsNotFound(err) {
            log.Info("creating resource", "kind", desc, "name", key.Name)
            return r.Create(ctx, obj)
        }
        return err
    }
    return nil // already exists, no update
}
```

Key points:
- `ctrl.SetControllerReference` sets the ClawObservability as the owner. When the CR is
  deleted, Kubernetes garbage collection deletes all owned resources.
- The function does **not** update existing resources -- it only creates missing ones. This
  means changes to the ClawObservability spec (e.g. switching Tempo from enabled to disabled)
  will not delete previously created resources. Deletion relies on garbage collection when
  the parent CR is deleted.

### RBAC for the Observability Controller

The controller's RBAC markers (`internal/controller/clawobservability_controller.go:47-53`)
generate ClusterRole rules:

```go
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
```

| API Group | Resource | Verbs | Why |
|---|---|---|---|
| `claw.clawbernetes.io` | `clawobservabilities` | full CRUD | Read and manage the CR itself |
| `claw.clawbernetes.io` | `clawobservabilities/status` | get, update, patch | Write `tempoReady` / `grafanaReady` |
| `claw.clawbernetes.io` | `clawobservabilities/finalizers` | update | Reserved for future cleanup finalizers |
| `apps` | `deployments` | full CRUD | Create/manage Tempo and Grafana Deployments |
| `""` (core) | `services` | full CRUD | Create/manage Tempo and Grafana Services |
| `""` (core) | `configmaps` | full CRUD | Create/manage config, datasource, and dashboard ConfigMaps |
| `""` (core) | `persistentvolumeclaims` | full CRUD | Reserved for future Tempo PVC storage |

The agent controller also needs read access to ClawObservability resources
(`internal/controller/clawagent_controller.go:57`):

```go
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch
```

This allows the agent reconciler to look up the referenced ClawObservability and extract the
OTLP endpoint without being able to modify it.
