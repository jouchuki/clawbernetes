# ClawSkillSet CRD Internals

## Section 1: Technical Summary

### What ClawSkillSet Is

ClawSkillSet is a namespaced Custom Resource Definition in the Clawbernetes operator that defines **reusable bundles of skill instructions**. Each skill is a block of markdown content (equivalent to a `SKILL.md` file) that gets mounted into agent pods at runtime. This lets you define operational guidelines, safety rules, or domain expertise once and share them across multiple ClawAgent instances.

A ClawSkillSet is a **passive resource** -- it has no dedicated controller. It exists purely as data that the `ClawAgentReconciler` reads when reconciling a `ClawAgent` that references it.

### Spec Fields

The spec contains a single field:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.skills` | `[]SkillEntry` | No (optional) | List of skills in this set |

Each `SkillEntry` has two required fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | The skill identifier, used as the directory name under `skills/` in the agent workspace |
| `content` | `string` | Yes | The skill's SKILL.md content -- raw markdown instructions |

**Go type definition** (`api/v1/clawskillset_types.go:23-38`):

```go
// SkillEntry defines a single skill with its name and content.
type SkillEntry struct {
	// name is the skill identifier (used as the directory name).
	// +required
	Name string `json:"name"`

	// content is the skill's SKILL.md content.
	// +required
	Content string `json:"content"`
}

// ClawSkillSetSpec defines the desired state of ClawSkillSet.
type ClawSkillSetSpec struct {
	// skills is the list of skills in this set.
	// +optional
	Skills []SkillEntry `json:"skills,omitempty"`
}
```

The status sub-resource contains only standard Kubernetes `conditions`:

```go
// ClawSkillSetStatus defines the observed state of ClawSkillSet.
type ClawSkillSetStatus struct {
	// conditions represent the current state of the ClawSkillSet resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

(`api/v1/clawskillset_types.go:41-48`)

### Full YAML Example

```yaml
# A ClawSkillSet bundles multiple SKILL.md files into a single resource.
# Each entry in the skills array becomes a separate directory in the agent
# workspace: workspace/skills/<name>/SKILL.md
apiVersion: claw.clawbernetes.io/v1
kind: ClawSkillSet
metadata:
  name: engineering-skills        # Referenced by ClawAgent spec.skillSet
  namespace: clawbernetes
spec:
  skills:
    - name: kubernetes-debugging  # -> workspace/skills/kubernetes-debugging/SKILL.md
      content: |
        # Kubernetes Debugging
        When debugging a Kubernetes issue, always start with kubectl describe
        before kubectl logs. Check events first. Never kubectl delete a pod
        you haven't diagnosed yet.
    - name: database-safety       # -> workspace/skills/database-safety/SKILL.md
      content: |
        # Database Safety
        Never run DROP, TRUNCATE, or DELETE without a WHERE clause.
        Always run SELECT first to verify what you're about to modify.
    - name: code-review           # -> workspace/skills/code-review/SKILL.md
      content: |
        # Code Review Standards
        Check for: missing error handling, hardcoded secrets, SQL injection
        vectors, missing input validation. Always comment on test coverage.
```

(Sample from `config/samples/claw_v1_clawskillset.yaml`)

### How an Agent References a Skill Set

A `ClawAgent` references a `ClawSkillSet` by name through its `spec.skillSet` field. Both resources must be in the same namespace.

**ClawAgent type definition** (`api/v1/clawagent_types.go:195-197`):

```go
// skillSet references a ClawSkillSet resource by name.
// +optional
SkillSet string `json:"skillSet,omitempty"`
```

Example ClawAgent manifest referencing the skill set above:

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: my-agent
  namespace: clawbernetes
spec:
  skillSet: engineering-skills    # Must match ClawSkillSet metadata.name
  # ... other agent config ...
```

### How Skills Are Resolved into a ConfigMap

When the `ClawAgentReconciler` reconciles a `ClawAgent`, it looks up the referenced `ClawSkillSet` and extracts its skills list. It then passes that list to `skillsConfigMap()`, which produces a ConfigMap where each skill name is a key and each skill's markdown content is the corresponding value.

**Reconcile function -- skill resolution** (`internal/controller/clawagent_controller.go:97-109`):

```go
// --- Resolve ClawSkillSet if referenced ---
var skills []clawv1.SkillEntry
if agent.Spec.SkillSet != "" {
    ss := &clawv1.ClawSkillSet{}
    ssKey := types.NamespacedName{Name: agent.Spec.SkillSet, Namespace: ns}
    if err := r.Get(ctx, ssKey, ss); err != nil {
        if !apierrors.IsNotFound(err) {
            return ctrl.Result{}, err
        }
        log.Info("referenced ClawSkillSet not found", "name", agent.Spec.SkillSet)
    } else {
        skills = ss.Spec.Skills
    }
}
```

**ConfigMap creation** (`internal/controller/clawagent_controller.go:169-172`):

```go
// --- Skills ConfigMap ---
skillsCM := r.skillsConfigMap(ns, name, skills)
if err := r.ensureResource(ctx, agent, skillsCM, "skills-configmap"); err != nil {
    return ctrl.Result{}, err
}
```

**The `skillsConfigMap` function** (`internal/controller/clawagent_controller.go:600-616`):

```go
func (r *ClawAgentReconciler) skillsConfigMap(ns, name string, skills []clawv1.SkillEntry) *corev1.ConfigMap {
	data := map[string]string{}
	for _, s := range skills {
		// ConfigMap keys can't contain '/', so we use a flat key and
		// mount with subPath in the deployment.
		data[s.Name] = s.Content
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-skills",
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Data: data,
	}
}
```

For an agent named `my-agent` with the `engineering-skills` ClawSkillSet, this produces a ConfigMap named `my-agent-skills` with three keys: `kubernetes-debugging`, `database-safety`, and `code-review`, each containing the corresponding markdown content.

### How the ConfigMap Is Mounted into the Pod

The skills ConfigMap is mounted as a volume in the agent Deployment's init container (`seed-workspace`), which then copies each skill file into the correct directory structure.

**Volume definition** (`internal/controller/clawagent_controller.go:774-782`):

```go
{
    Name: "skills-src",
    VolumeSource: corev1.VolumeSource{
        ConfigMap: &corev1.ConfigMapVolumeSource{
            LocalObjectReference: corev1.LocalObjectReference{Name: name + "-skills"},
            Optional:             boolPtr(true),
        },
    },
},
```

Note `Optional: true` -- if no skill set is referenced (the ConfigMap is empty or does not exist), the pod still starts without error.

**Init container mount and copy logic** (`internal/controller/clawagent_controller.go:678-699`):

```go
// Second init container: seed workspace with config, identity, and skills.
seedWorkspace := corev1.Container{
    Name:  "seed-workspace",
    Image: "busybox:1.36",
    Command: []string{"sh", "-c", strings.Join([]string{
        "mkdir -p /openclaw-home/workspace/skills /openclaw-home/workspace/plugins",
        // ... other copy commands ...
        "for f in /skills-src/*; do [ -f \"$f\" ] && skill=$(basename \"$f\") && " +
            "mkdir -p /openclaw-home/workspace/skills/$skill && " +
            "cp \"$f\" /openclaw-home/workspace/skills/$skill/SKILL.md; done || true",
        "echo 'workspace seeded'",
    }, " && ")},
    VolumeMounts: []corev1.VolumeMount{
        {Name: "config-src", MountPath: "/config-src", ReadOnly: true},
        {Name: "identity-src", MountPath: "/identity-src", ReadOnly: true},
        {Name: "skills-src", MountPath: "/skills-src", ReadOnly: true},
        {Name: "openclaw-home", MountPath: "/openclaw-home"},
    },
}
```

The shell loop iterates over every file in `/skills-src/` (where the ConfigMap is mounted flat), extracts the basename (the skill name), creates a subdirectory under `workspace/skills/`, and copies the file as `SKILL.md`. The resulting filesystem layout inside the pod:

```
/home/node/.openclaw/workspace/
  skills/
    kubernetes-debugging/
      SKILL.md          <- content from the ConfigMap key "kubernetes-debugging"
    database-safety/
      SKILL.md          <- content from the ConfigMap key "database-safety"
    code-review/
      SKILL.md          <- content from the ConfigMap key "code-review"
```

OpenClaw discovers skill files by scanning `workspace/skills/*/SKILL.md` at startup.

---

## Section 2: Kubernetes Internals Deep Dive

### ClawSkillSet Has No Dedicated Controller

ClawSkillSet is a **passive resource**. There is no `ClawSkillSetReconciler`. The CRD is registered with the API server (via `SchemeBuilder.Register` in `api/v1/clawskillset_types.go:80`), so you can create, read, update, and delete ClawSkillSet objects, but nothing watches them directly.

Instead, the `ClawAgentReconciler` in `internal/controller/clawagent_controller.go` consumes ClawSkillSet objects during its own reconciliation loop. This is a common Kubernetes pattern for "config-like" CRDs that exist solely to be referenced by other resources.

### How the Agent Reconciler Resolves the Skill Set

The resolution happens early in `Reconcile()`, at lines 97-109 of `internal/controller/clawagent_controller.go`.

The reconciler:

1. Checks if `agent.Spec.SkillSet` is non-empty.
2. Constructs a `NamespacedName` using the agent's own namespace (skill sets must live in the same namespace as the agent).
3. Calls `r.Get()` to fetch the `ClawSkillSet` object from the API server.
4. If not found, logs an informational message and proceeds with an empty skills slice -- this is a **soft failure**, not a hard error.
5. If found, extracts `ss.Spec.Skills` into the local `skills` variable.

```go
var skills []clawv1.SkillEntry
if agent.Spec.SkillSet != "" {
    ss := &clawv1.ClawSkillSet{}
    ssKey := types.NamespacedName{Name: agent.Spec.SkillSet, Namespace: ns}
    if err := r.Get(ctx, ssKey, ss); err != nil {
        if !apierrors.IsNotFound(err) {
            return ctrl.Result{}, err       // real API error -> retry
        }
        log.Info("referenced ClawSkillSet not found", "name", agent.Spec.SkillSet)
    } else {
        skills = ss.Spec.Skills             // success -> use these skills
    }
}
```

The `skills` slice then flows through the rest of the reconciliation: it is passed to `skillsConfigMap()`, which builds the ConfigMap, which is then applied via `ensureResource()`.

### The `skillsConfigMap` Function

Located at `internal/controller/clawagent_controller.go:600-616`.

```go
func (r *ClawAgentReconciler) skillsConfigMap(ns, name string, skills []clawv1.SkillEntry) *corev1.ConfigMap {
	data := map[string]string{}
	for _, s := range skills {
		data[s.Name] = s.Content
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-skills",
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Data: data,
	}
}
```

Key details:

- **Naming convention**: The ConfigMap is named `<agent-name>-skills`. For an agent named `debug-bot`, the ConfigMap is `debug-bot-skills`.
- **Key mapping**: Each `SkillEntry.Name` becomes a key in the ConfigMap's `Data` map. The corresponding `SkillEntry.Content` is the value. ConfigMap keys cannot contain `/`, so the skill name is used as-is (flat keys).
- **Empty case**: If `skills` is empty (no skill set referenced or the referenced one was not found), the ConfigMap is created with an empty `Data` map. The volume mount uses `Optional: true`, so this is harmless.
- **Ownership**: The ConfigMap is created via `ensureResource()`, which sets the `ClawAgent` as the owner reference. This means the ConfigMap is garbage-collected when the agent is deleted.

### How the Skills ConfigMap Is Mounted as a Volume

The agent Deployment defines a volume named `skills-src` backed by the skills ConfigMap (`internal/controller/clawagent_controller.go:774-782`):

```go
{
    Name: "skills-src",
    VolumeSource: corev1.VolumeSource{
        ConfigMap: &corev1.ConfigMapVolumeSource{
            LocalObjectReference: corev1.LocalObjectReference{Name: name + "-skills"},
            Optional:             boolPtr(true),
        },
    },
},
```

The `Optional: true` flag is critical -- it ensures the pod can start even if the ConfigMap does not exist (which happens when no skill set is referenced).

The `seed-workspace` init container mounts this volume at `/skills-src` (read-only) and runs a shell loop to fan out the flat ConfigMap keys into the directory structure OpenClaw expects (`internal/controller/clawagent_controller.go:691`):

```sh
for f in /skills-src/*; do
  [ -f "$f" ] && \
  skill=$(basename "$f") && \
  mkdir -p /openclaw-home/workspace/skills/$skill && \
  cp "$f" /openclaw-home/workspace/skills/$skill/SKILL.md
done || true
```

The `|| true` at the end ensures the init container does not fail if the directory is empty (no skills).

### The Mount Path and How OpenClaw Discovers Skill Files

The final mount path in the running container is:

```
/home/node/.openclaw/workspace/skills/<skill-name>/SKILL.md
```

OpenClaw discovers skills by scanning `workspace/skills/*/SKILL.md` relative to its home directory at startup. Each discovered `SKILL.md` is loaded and made available as an instruction set the agent can follow. The directory name (`<skill-name>`) serves as the skill's identifier within OpenClaw.

### Watch Setup and Re-Reconciliation Behavior

The `SetupWithManager` function at the bottom of the controller (`internal/controller/clawagent_controller.go:1537-1545`) defines what triggers reconciliation:

```go
func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Named("clawagent").
		Complete(r)
}
```

The controller watches:

- `ClawAgent` objects (primary resource, via `For`)
- Owned `Deployment`, `ConfigMap`, and `Service` objects (via `Owns`)

**There is no `Watches()` call for `ClawSkillSet`.** This means:

- **Updating a ClawSkillSet does NOT automatically trigger re-reconciliation of agents that reference it.** The controller only reconciles when the `ClawAgent` itself changes, or when one of its owned resources (Deployment, ConfigMap, Service) changes.
- To pick up changes to a ClawSkillSet, you must either:
  1. Trigger a reconciliation of the referencing ClawAgent (e.g., add an annotation, touch any spec field).
  2. Delete the skills ConfigMap (owned by the agent), which will trigger the `Owns(&corev1.ConfigMap{})` watch and re-reconcile.
  3. Restart the controller.

This is a deliberate design tradeoff -- adding a `Watches()` for ClawSkillSet would require implementing an index or mapper to look up which agents reference a given skill set, adding complexity. The current design treats skills as a "set once, apply on next agent reconcile" pattern.

To add automatic propagation in the future, you would add something like:

```go
Watches(&clawv1.ClawSkillSet{}, handler.EnqueueRequestsFromMapFunc(
    func(ctx context.Context, obj client.Object) []ctrl.Request {
        // List all ClawAgents that reference this skill set name
        // Return reconcile requests for each matching agent
    },
))
```

### RBAC

The `ClawAgentReconciler` needs read access to ClawSkillSet resources. This is declared via the kubebuilder RBAC marker at `internal/controller/clawagent_controller.go:58`:

```go
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawskillsets,verbs=get;list;watch
```

This generates a `ClusterRole` rule granting `get`, `list`, and `watch` on `clawskillsets` in the `claw.clawbernetes.io` API group. The controller only reads ClawSkillSet resources -- it never creates, updates, or deletes them. Skill sets are entirely user-managed.
