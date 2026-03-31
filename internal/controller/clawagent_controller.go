/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

const openclawImage = "clawbernetes/openclaw:latest"

// errRequeueNeeded signals the reconciler to requeue after a short delay.
var errRequeueNeeded = errors.New("requeue needed")

// ClawAgentReconciler reconciles a ClawAgent object
type ClawAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawskillsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *ClawAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ClawAgent instance.
	agent := &clawv1.ClawAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	ns := agent.Namespace
	name := agent.Name

	// --- Resolve the OTLP endpoint from the referenced ClawObservability ---
	otlpEndpoint := ""
	if agent.Spec.Observability != "" {
		obs := &clawv1.ClawObservability{}
		obsKey := types.NamespacedName{Name: agent.Spec.Observability, Namespace: ns}
		if err := r.Get(ctx, obsKey, obs); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("referenced ClawObservability not found, skipping OTEL injection", "name", agent.Spec.Observability)
		} else {
			otlpEndpoint = obs.Spec.OTLPEndpoint
		}
	}

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

	// --- Resolve ClawGateway if referenced ---
	gatewayURL := ""
	var gateway *clawv1.ClawGateway
	if agent.Spec.Gateway != "" {
		gw := &clawv1.ClawGateway{}
		gwKey := types.NamespacedName{Name: agent.Spec.Gateway, Namespace: ns}
		if err := r.Get(ctx, gwKey, gw); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("referenced ClawGateway not found, skipping gateway routing", "name", agent.Spec.Gateway)
		} else {
			gateway = gw
			port := gw.Spec.Port
			if port == 0 {
				port = 8443
			}
			gatewayURL = fmt.Sprintf("http://%s-gateway.%s.svc.cluster.local:%d", agent.Spec.Gateway, ns, port)
		}
	}

	// --- Resolve ClawPolicy if referenced ---
	var policy *clawv1.ClawPolicy
	if agent.Spec.Policy != "" {
		pol := &clawv1.ClawPolicy{}
		polKey := types.NamespacedName{Name: agent.Spec.Policy, Namespace: ns}
		if err := r.Get(ctx, polKey, pol); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("referenced ClawPolicy not found", "name", agent.Spec.Policy)
		} else {
			policy = pol
		}
	}

	// --- Identity ConfigMap (SOUL.md, USER.md, IDENTITY.md) ---
	identityCM := r.identityConfigMap(agent, ns, name)
	if err := r.ensureResource(ctx, agent, identityCM, "identity-configmap"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Skills ConfigMap ---
	skillsCM := r.skillsConfigMap(ns, name, skills)
	if err := r.ensureResource(ctx, agent, skillsCM, "skills-configmap"); err != nil {
		return ctrl.Result{}, err
	}

	// --- OpenClaw config ConfigMap (openclaw.json + HEARTBEAT.md) ---
	configCM := r.openclawConfigMap(agent, ns, name, gatewayURL, otlpEndpoint, policy, gateway)
	if err := r.ensureResource(ctx, agent, configCM, "openclaw-config-configmap"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Workspace storage ---
	// Resolve the active PVC name. After a resize it may be {name}-home-v2.
	activePVC := agent.Status.WorkspacePVC
	if activePVC == "" && agent.Spec.Workspace.IsPersistent() {
		activePVC = name + clawv1.PVCSuffix
	}

	if agent.Spec.Workspace.IsPersistent() {
		if err := r.ensurePVC(ctx, agent, ns, name); err != nil {
			if errors.Is(err, errRequeueNeeded) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}
		// Re-read status in case ensurePVC updated it (resize).
		activePVC = agent.Status.WorkspacePVC
		if activePVC == "" {
			activePVC = name + clawv1.PVCSuffix
		}
	} else {
		activePVC = ""
		// Ephemeral mode: delete any leftover PVC from a previous persistent config.
		if err := r.cleanupOrphanedPVC(ctx, ns, name); err != nil {
			return ctrl.Result{}, err
		}
	}

	// --- Agent Deployment ---
	dep := r.agentDeployment(agent, ns, name, otlpEndpoint, gatewayURL, skills, activePVC)
	if err := r.ensureResource(ctx, agent, dep, "agent-deployment"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Agent Service ---
	svc := r.agentService(ns, name)
	if err := r.ensureResource(ctx, agent, svc, "agent-service"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Status ---
	phase, podName := r.resolveAgentStatus(ctx, ns, name)
	agent.Status.Phase = phase
	agent.Status.PodName = podName
	agent.Status.WorkspacePVC = activePVC
	if err := r.Status().Update(ctx, agent); err != nil {
		log.Error(err, "unable to update ClawAgent status")
		return ctrl.Result{}, err
	}

	log.Info("reconciled ClawAgent", "name", name, "phase", phase)

	// --- Regenerate fleet dashboard HTML ---
	if err := r.updateFleetDashboard(ctx, ns); err != nil {
		log.Error(err, "unable to update fleet dashboard")
		// Non-fatal: don't block reconciliation
	}

	return ctrl.Result{}, nil
}

// updateFleetDashboard lists all ClawAgents, resolves their policies, and
// writes the fleet config HTML to a shared ConfigMap.
func (r *ClawAgentReconciler) updateFleetDashboard(ctx context.Context, ns string) error {
	agentList := &clawv1.ClawAgentList{}
	if err := r.List(ctx, agentList, client.InNamespace(ns)); err != nil {
		return err
	}

	var infos []agentInfo
	for _, a := range agentList.Items {
		fallback := ""
		if a.Spec.Model.Fallback != nil {
			fallback = a.Spec.Model.Fallback.Name
		}
		hibernate := 0
		if a.Spec.Lifecycle.HibernateAfterIdleMinutes != nil {
			hibernate = *a.Spec.Lifecycle.HibernateAfterIdleMinutes
		}
		info := agentInfo{
			Name:          a.Name,
			Phase:         a.Status.Phase,
			Provider:      a.Spec.Model.Provider,
			Model:         a.Spec.Model.Name,
			FallbackModel: fallback,
			Gateway:       a.Spec.Gateway,
			Policy:        a.Spec.Policy,
			SkillSet:      a.Spec.SkillSet,
			Observability: a.Spec.Observability,
			Connectors:    a.Spec.Connectors,
			RestartPolicy: a.Spec.Lifecycle.RestartPolicy,
			MaxRuntime:    a.Spec.Lifecycle.MaxRuntime,
			IdleHibernate: hibernate,
		}

		soul := a.Spec.Identity.Soul
		if len(soul) > 100 {
			soul = soul[:100] + "..."
		}
		info.Soul = soul

		// Resolve policy for budget/tool info
		if a.Spec.Policy != "" {
			pol := &clawv1.ClawPolicy{}
			if err := r.Get(ctx, types.NamespacedName{Name: a.Spec.Policy, Namespace: ns}, pol); err == nil {
				info.BudgetDaily = pol.Spec.Budget.Daily
				info.BudgetMonthly = pol.Spec.Budget.Monthly
				info.DowngradeModel = pol.Spec.Budget.DowngradeModel
				info.ToolDeny = pol.Spec.ToolPolicy.Deny
			}
		}

		infos = append(infos, info)
	}

	html := generateFleetDashboardHTML(infos)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fleet-dashboard",
			Namespace: ns,
			Labels:    map[string]string{"app": "fleet-dashboard", "app.kubernetes.io/managed-by": "clawbernetes"},
		},
		Data: map[string]string{
			"index.html": html,
		},
	}

	key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
	existing := &corev1.ConfigMap{}
	if err := r.Get(ctx, key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, cm)
		}
		return err
	}
	existing.Data = cm.Data
	return r.Update(ctx, existing)
}

// ---------------------------------------------------------------------------
// Resource helpers
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) ensureResource(ctx context.Context, owner *clawv1.ClawAgent, obj client.Object, desc string) error {
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
	return nil
}

func (r *ClawAgentReconciler) ensurePVC(ctx context.Context, owner *clawv1.ClawAgent, ns, name string) error {
	log := logf.FromContext(ctx)

	// Use the active PVC name from status if set (e.g. after a resize), otherwise default.
	pvcName := owner.Status.WorkspacePVC
	if pvcName == "" {
		pvcName = name + clawv1.PVCSuffix
	}

	storageSize := owner.Spec.Workspace.ResolvedStorageSize()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if owner.Spec.Workspace.StorageClassName != nil {
		pvc.Spec.StorageClassName = owner.Spec.Workspace.StorageClassName
	}

	// Only set owner reference when reclaimPolicy=delete. When retain (default),
	// the PVC lives independently so it survives agent CR deletion.
	reclaimPolicy := owner.Spec.Workspace.ResolvedReclaimPolicy()
	if reclaimPolicy == clawv1.ReclaimPolicyDelete {
		if err := ctrl.SetControllerReference(owner, pvc, r.Scheme); err != nil {
			return fmt.Errorf("setting owner reference on PVC: %w", err)
		}
	}

	key := types.NamespacedName{Name: pvcName, Namespace: ns}
	existing := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating PVC for persistent workspace", "pvc", pvcName, "reclaimPolicy", reclaimPolicy)
			return r.Create(ctx, pvc)
		}
		return err
	}

	// PVC already exists — sync owner references to match current reclaimPolicy.
	needsUpdate := false
	if reclaimPolicy == clawv1.ReclaimPolicyDelete && len(existing.OwnerReferences) == 0 {
		if err := ctrl.SetControllerReference(owner, existing, r.Scheme); err != nil {
			return fmt.Errorf("adding owner reference to existing PVC: %w", err)
		}
		needsUpdate = true
		log.Info("adding owner reference to PVC (reclaimPolicy changed to delete)", "pvc", pvcName)
	} else if reclaimPolicy == clawv1.ReclaimPolicyRetain && len(existing.OwnerReferences) > 0 {
		existing.OwnerReferences = nil
		needsUpdate = true
		log.Info("removing owner reference from PVC (reclaimPolicy changed to retain)", "pvc", pvcName)
	}

	if needsUpdate {
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("updating PVC owner references: %w", err)
		}
	}

	// Check if PVC needs resizing.
	requestedSize := resource.MustParse(storageSize)
	existingSize := existing.Spec.Resources.Requests[corev1.ResourceStorage]
	if requestedSize.Cmp(existingSize) > 0 {
		return r.resizePVC(ctx, owner, existing, ns, name, pvcName, requestedSize, reclaimPolicy)
	} else if requestedSize.Cmp(existingSize) < 0 {
		log.Info("WARNING: requested storage size is smaller than existing PVC — cannot shrink",
			"pvc", pvcName, "existing", existingSize.String(), "requested", requestedSize.String())
	}

	return nil
}

// resizePVC handles PVC expansion by creating a new PVC, migrating data via a
// one-shot pod, then deleting the old PVC and updating the deployment to use
// the new one. The deployment's volume reference uses the name from
// status.workspacePVC so it tracks the active PVC across resizes.
//
// State machine (reentrant across reconcile loops):
//  1. New PVC doesn't exist         → create it, scale deployment to 0
//  2. Migration pod doesn't exist   → create it (mounts old + new)
//  3. Migration pod succeeded       → delete old PVC, delete pod, update status
//  4. Migration pod failed          → delete pod, return error (retry on next reconcile)
//  5. Migration pod still running   → wait
func (r *ClawAgentReconciler) resizePVC(ctx context.Context, owner *clawv1.ClawAgent, oldPVC *corev1.PersistentVolumeClaim, ns, name, pvcName string, newSize resource.Quantity, reclaimPolicy string) error {
	log := logf.FromContext(ctx)
	newPVCName := name + clawv1.PVCResizeSuffix
	migratePodName := name + "-pvc-migrate"

	// Step 1: Scale deployment to 0 so the old PVC is released (RWO).
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, dep); err == nil {
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 {
			zero := int32(0)
			dep.Spec.Replicas = &zero
			if err := r.Update(ctx, dep); err != nil {
				return fmt.Errorf("scaling deployment to 0 for PVC resize: %w", err)
			}
			log.Info("scaled deployment to 0 for PVC resize", "deployment", name)
			return errRequeueNeeded
		}
	}

	// Step 2: Ensure new PVC exists.
	newPVC := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: newPVCName, Namespace: ns}, newPVC); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		newPVC = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newPVCName,
				Namespace: ns,
				Labels:    agentLabels(name),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: newSize,
					},
				},
			},
		}
		if owner.Spec.Workspace.StorageClassName != nil {
			newPVC.Spec.StorageClassName = owner.Spec.Workspace.StorageClassName
		}
		if reclaimPolicy == clawv1.ReclaimPolicyDelete {
			if err := ctrl.SetControllerReference(owner, newPVC, r.Scheme); err != nil {
				return fmt.Errorf("setting owner reference on new PVC: %w", err)
			}
		}
		log.Info("creating new PVC for resize", "pvc", newPVCName, "size", newSize.String())
		return r.Create(ctx, newPVC)
	}

	// Step 3: Ensure migration pod exists.
	migratePod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: migratePodName, Namespace: ns}, migratePod); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		migratePod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      migratePodName,
				Namespace: ns,
				Labels:    agentLabels(name),
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser:  int64Ptr(1000),
					RunAsGroup: int64Ptr(1000),
					FSGroup:    int64Ptr(1000),
				},
				Containers: []corev1.Container{
					{
						Name:    "migrate",
						Image:   "busybox:1.36",
						Command: []string{"sh", "-c", "cp -a /src/. /dst/ && echo 'migration complete'"},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "src", MountPath: "/src", ReadOnly: true},
							{Name: "dst", MountPath: "/dst"},
						},
					},
				},
				Volumes: []corev1.Volume{
					{Name: "src", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName, ReadOnly: true},
					}},
					{Name: "dst", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: newPVCName},
					}},
				},
			},
		}
		log.Info("creating migration pod", "pod", migratePodName, "from", pvcName, "to", newPVCName)
		return r.Create(ctx, migratePod)
	}

	// Step 4: Check migration pod status.
	switch migratePod.Status.Phase {
	case corev1.PodSucceeded:
		log.Info("migration complete — cleaning up old PVC", "oldPVC", pvcName, "newPVC", newPVCName)
		// Delete migration pod.
		_ = r.Delete(ctx, migratePod)
		// Delete old PVC.
		if err := r.Delete(ctx, oldPVC); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		// Update status to point to new PVC. The deployment will be recreated
		// on next reconcile with the new PVC name via openclawHomeVolume().
		owner.Status.WorkspacePVC = newPVCName
		log.Info("PVC resize complete", "newPVC", newPVCName, "size", newSize.String())
		return nil

	case corev1.PodFailed:
		log.Error(fmt.Errorf("migration pod failed"), "PVC resize failed", "pod", migratePodName)
		_ = r.Delete(ctx, migratePod)
		return fmt.Errorf("PVC resize migration failed — check logs for pod %s", migratePodName)

	default:
		log.Info("PVC resize migration in progress", "pod", migratePodName)
		return errRequeueNeeded
	}
}

func (r *ClawAgentReconciler) cleanupOrphanedPVC(ctx context.Context, ns, name string) error {
	log := logf.FromContext(ctx)

	// Clean up both possible PVC names (original and resized).
	for _, suffix := range []string{clawv1.PVCSuffix, clawv1.PVCResizeSuffix} {
		pvcName := name + suffix
		existing := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ns}, existing); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		log.Info("deleting orphaned PVC from previous persistent workspace", "pvc", pvcName)
		if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Identity ConfigMap — seeds SOUL.md, USER.md, IDENTITY.md
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) identityConfigMap(agent *clawv1.ClawAgent, ns, name string) *corev1.ConfigMap {
	data := map[string]string{}
	if agent.Spec.Identity.Soul != "" {
		data["SOUL.md"] = agent.Spec.Identity.Soul
	}
	if agent.Spec.Identity.User != "" {
		data["USER.md"] = agent.Spec.Identity.User
	}
	if agent.Spec.Identity.AgentIdentity != "" {
		data["IDENTITY.md"] = agent.Spec.Identity.AgentIdentity
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-identity",
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Data: data,
	}
}

// ---------------------------------------------------------------------------
// Skills ConfigMap — one key per skill: <skill-name>/SKILL.md
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// OpenClaw config ConfigMap — openclaw.json + HEARTBEAT.md
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) openclawConfigMap(agent *clawv1.ClawAgent, ns, name, gatewayURL, otlpEndpoint string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-openclaw-config",
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Data: map[string]string{
			"openclaw.json": r.buildOpenclawConfig(agent, name, gatewayURL, otlpEndpoint, policy, gateway),
			"HEARTBEAT.md":  r.heartbeatMD(name),
		},
	}
}

// ---------------------------------------------------------------------------
// Agent Deployment
// ---------------------------------------------------------------------------

const openclawGatewayPort = 18789

func (r *ClawAgentReconciler) agentDeployment(agent *clawv1.ClawAgent, ns, name, otlpEndpoint, gatewayURL string, skills []clawv1.SkillEntry, activePVC string) *appsv1.Deployment {
	labels := agentLabels(name)
	replicas := int32(1)

	// First init container: copy baked-in extensions (observeclaw) from
	// the openclaw image into the shared emptyDir volume.
	copyExtensions := corev1.Container{
		Name:            "copy-extensions",
		Image:           openclawImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"sh", "-c",
			"cp -r /home/node/.openclaw/extensions /openclaw-home/extensions 2>/dev/null || true && echo 'extensions copied'",
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "openclaw-home", MountPath: "/openclaw-home"},
		},
	}

	// Second init container: seed workspace with config, identity, and skills.
	seedWorkspace := corev1.Container{
		Name:  "seed-workspace",
		Image: "busybox:1.36",
		Command: []string{"sh", "-c", strings.Join([]string{
			"mkdir -p /openclaw-home/workspace/skills",
			"cp /config-src/openclaw.json /openclaw-home/openclaw.json",
			"cp /config-src/HEARTBEAT.md /openclaw-home/workspace/HEARTBEAT.md",
			"cp /identity-src/SOUL.md /openclaw-home/workspace/SOUL.md 2>/dev/null || true",
			"cp /identity-src/USER.md /openclaw-home/workspace/USER.md 2>/dev/null || true",
			"cp /identity-src/IDENTITY.md /openclaw-home/workspace/IDENTITY.md 2>/dev/null || true",
			"for f in /skills-src/*; do [ -f \"$f\" ] && skill=$(basename \"$f\") && mkdir -p /openclaw-home/workspace/skills/$skill && cp \"$f\" /openclaw-home/workspace/skills/$skill/SKILL.md; done || true",
			"echo 'workspace seeded'",
		}, " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config-src", MountPath: "/config-src", ReadOnly: true},
			{Name: "identity-src", MountPath: "/identity-src", ReadOnly: true},
			{Name: "skills-src", MountPath: "/skills-src", ReadOnly: true},
			{Name: "openclaw-home", MountPath: "/openclaw-home"},
		},
	}

	// --- Environment variables ---
	env := []corev1.EnvVar{
		{Name: "OTEL_SERVICE_NAME", Value: name},
	}
	if otlpEndpoint != "" {
		env = append(env,
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: otlpEndpoint},
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_HEADERS", Value: ""},
		)
	}

	// --- Main container ---
	mainContainer := corev1.Container{
		Name:            "openclaw",
		Image:           openclawImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{Name: "gateway", ContainerPort: int32(openclawGatewayPort), Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(openclawGatewayPort),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(openclawGatewayPort),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       30,
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "openclaw-home", MountPath: "/home/node/.openclaw"},
		},
	}

	// Apply resource requests/limits from the spec.
	if agent.Spec.Resources.Requests != nil || agent.Spec.Resources.Limits != nil {
		mainContainer.Resources = corev1.ResourceRequirements{
			Requests: agent.Spec.Resources.Requests,
			Limits:   agent.Spec.Resources.Limits,
		}
	}

	// --- Volumes ---
	volumes := []corev1.Volume{
		openclawHomeVolume(activePVC, agent.Spec.Workspace.Mode),
		{
			Name: "config-src",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-openclaw-config"},
				},
			},
		},
		{
			Name: "identity-src",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-identity"},
				},
			},
		},
		{
			Name: "skills-src",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-skills"},
					Optional:             boolPtr(true),
				},
			},
		},
	}

	// Mount integration credentials as a read-only secret volume.
	if agent.Spec.CredentialsSecret != "" {
		credMode := int32(0400)
		volumes = append(volumes, corev1.Volume{
			Name: "credentials-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  agent.Spec.CredentialsSecret,
					DefaultMode: &credMode,
					Optional:    boolPtr(true),
				},
			},
		})
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "credentials-secret",
			MountPath: "/home/node/.openclaw/credentials",
			ReadOnly:  true,
		})
	}

	// --- RestartPolicy ---
	restartPolicy := corev1.RestartPolicyAlways
	if agent.Spec.Lifecycle.RestartPolicy != "" {
		restartPolicy = corev1.RestartPolicy(agent.Spec.Lifecycle.RestartPolicy)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  int64Ptr(1000),
						RunAsGroup: int64Ptr(1000),
						FSGroup:    int64Ptr(1000),
					},
					RestartPolicy:  restartPolicy,
					InitContainers: []corev1.Container{copyExtensions, seedWorkspace},
					Containers:     []corev1.Container{mainContainer},
					Volumes:        volumes,
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Status resolution
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) resolveAgentStatus(ctx context.Context, ns, name string) (phase, podName string) {
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, dep); err != nil {
		return "Pending", ""
	}

	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
			// Derive pod name from the ReplicaSet.
			podName = fmt.Sprintf("%s (replicas: %d)", name, dep.Status.ReadyReplicas)
			return "Running", podName
		}
	}

	if dep.Status.UnavailableReplicas > 0 {
		return "Progressing", ""
	}
	return "Pending", ""
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func agentLabels(name string) map[string]string {
	return map[string]string{
		"app":                          name,
		"clawbernetes.io/agent":        name,
		"app.kubernetes.io/managed-by": "clawbernetes",
	}
}

func openclawHomeVolume(pvcName, mode string) corev1.Volume {
	if mode == clawv1.WorkspaceModePersistent && pvcName != "" {
		return corev1.Volume{
			Name: "openclaw-home",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		}
	}
	return corev1.Volume{
		Name: "openclaw-home",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

// ---------------------------------------------------------------------------
// Agent Service
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) agentService(ns, name string) *corev1.Service {
	labels := agentLabels(name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "gateway", Port: int32(openclawGatewayPort), TargetPort: intstr.FromInt(openclawGatewayPort), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// OpenClaw config generation
// ---------------------------------------------------------------------------

// buildOpenclawConfig generates the full openclaw.json for the agent,
// including the observeclaw plugin config derived from ClawPolicy + ClawGateway.
func (r *ClawAgentReconciler) buildOpenclawConfig(agent *clawv1.ClawAgent, name, gatewayURL, otlpEndpoint string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway) string {
	cfg := map[string]any{
		"gateway": map[string]any{
			"port": openclawGatewayPort,
			"bind": "lan",
			"http": map[string]any{
				"endpoints": map[string]any{
					"chatCompletions": map[string]any{"enabled": true},
					"responses":       map[string]any{"enabled": true},
				},
			},
			"controlUi": map[string]any{
				"allowedOrigins": []string{
					fmt.Sprintf("http://%s.local", name),
					fmt.Sprintf("http://%s.local:8080", name),
					fmt.Sprintf("http://localhost:%d", openclawGatewayPort),
					fmt.Sprintf("http://127.0.0.1:%d", openclawGatewayPort),
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{
				"heartbeat": map[string]any{
					"every":           "5m",
					"lightContext":    true,
					"isolatedSession": true,
					"ackMaxChars":     300,
				},
			},
			"list": []map[string]any{
				{"id": name, "default": true},
			},
		},
	}

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

		// Wire TelemetryCaptureSpec — default everything on, let spec override.
		tc := agent.Spec.TelemetryCapture
		captureContent := map[string]any{
			"inputMessages":      true,
			"outputMessages":     true,
			"systemInstructions": true,
			"toolDefinitions":    true,
			"toolContent":        true,
		}
		// If any field is explicitly set on the spec, use those values instead.
		if tc.InputMessages || tc.OutputMessages || tc.SystemInstructions || tc.ToolDefinitions || tc.ToolContent {
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

	// --- Register a gateway-proxied Anthropic provider ---
	if gatewayURL != "" {
		cfg["models"] = map[string]any{
			"providers": map[string]any{
				"gateway-anthropic": map[string]any{
					"baseUrl": gatewayURL,
					"api":     "anthropic-messages",
					"apiKey":  "gateway-managed", // sentinel — gateway injects the real key server-side
					"models": []map[string]any{
						{
							"id":            "claude-sonnet-4-6",
							"name":          "Claude Sonnet 4.6 (via gateway)",
							"reasoning":     true,
							"input":         []string{"text"},
							"contextWindow": 200000,
							"maxTokens":     16384,
						},
						{
							"id":            "claude-haiku-4-5",
							"name":          "Claude Haiku 4.5 (via gateway)",
							"reasoning":     false,
							"input":         []string{"text"},
							"contextWindow": 200000,
							"maxTokens":     8192,
						},
					},
				},
			},
		}
	}

	// --- Build observeclaw plugin config from ClawPolicy + ClawGateway ---
	observeclawCfg := r.buildObserveclawConfig(agent, name, gatewayURL, policy, gateway)

	pluginEntries := map[string]any{
		"observeclaw": map[string]any{
			"enabled": true,
			"config":  observeclawCfg,
		},
	}

	// Enable the bundled diagnostics-otel extension.
	if otlpEndpoint != "" {
		pluginEntries["diagnostics-otel"] = map[string]any{
			"enabled": true,
		}
	}

	cfg["plugins"] = map[string]any{
		"enabled": true,
		"entries": pluginEntries,
	}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

// buildObserveclawConfig maps ClawPolicy + ClawGateway CRD fields to the
// observeclaw plugin configSchema (see openclaw.plugin.json in
// github.com/ai-trust-layer/observeclaw).
func (r *ClawAgentReconciler) buildObserveclawConfig(agent *clawv1.ClawAgent, agentName, gatewayURL string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway) map[string]any {
	cfg := map[string]any{
		"enabled":  true,
		"currency": "USD",
	}

	// --- Budgets from ClawPolicy ---
	budgetDefaults := map[string]any{
		"daily":   100,
		"monthly": 2000,
		"warnAt":  0.8,
	}
	downgradeModel := "claude-haiku-4-5"
	downgradeProvider := "anthropic"

	if policy != nil {
		b := policy.Spec.Budget
		if b.Daily > 0 {
			budgetDefaults["daily"] = b.Daily
		}
		if b.Monthly > 0 {
			budgetDefaults["monthly"] = b.Monthly
		}
		if b.WarnAt != "" {
			if warnAt, err := strconv.ParseFloat(b.WarnAt, 64); err == nil {
				budgetDefaults["warnAt"] = warnAt
			}
		}
		if b.DowngradeModel != "" {
			downgradeModel = b.DowngradeModel
		}
		if b.DowngradeProvider != "" {
			downgradeProvider = b.DowngradeProvider
		}
	}

	cfg["budgets"] = map[string]any{
		"defaults": budgetDefaults,
		"agents":   map[string]any{},
	}
	cfg["downgradeModel"] = downgradeModel
	cfg["downgradeProvider"] = downgradeProvider

	// --- Tool policy from ClawPolicy ---
	toolDefaults := map[string]any{
		"allow": []string{},
		"deny":  []string{},
	}
	if policy != nil {
		tp := policy.Spec.ToolPolicy
		if len(tp.Allow) > 0 {
			toolDefaults["allow"] = tp.Allow
		}
		if len(tp.Deny) > 0 {
			toolDefaults["deny"] = tp.Deny
		}
	}
	// Auto-deny credential file access to prevent the LLM from
	// exfiltrating mounted integration secrets via file tools.
	if agent.Spec.CredentialsSecret != "" {
		denyList, _ := toolDefaults["deny"].([]string)
		denyList = append(denyList,
			"/home/node/.openclaw/credentials/*",
			"cat.*credentials",
			"grep.*credentials",
			"head.*credentials",
			"tail.*credentials",
			"less.*credentials",
			"base64.*credentials",
		)
		toolDefaults["deny"] = denyList
	}

	cfg["toolPolicy"] = map[string]any{
		"defaults": toolDefaults,
		"agents":   map[string]any{},
	}

	// --- Anomaly detection from ClawGateway ---
	anomalyCfg := map[string]any{
		"spendSpikeMultiplier":     3,
		"idleBurnMinutes":          10,
		"errorLoopThreshold":       10,
		"tokenInflationMultiplier": 2,
		"checkIntervalSeconds":     30,
	}
	if gateway != nil {
		a := gateway.Spec.Anomaly
		if a.SpendSpikeMultiplier > 0 {
			anomalyCfg["spendSpikeMultiplier"] = a.SpendSpikeMultiplier
		}
		if a.IdleBurnMinutes > 0 {
			anomalyCfg["idleBurnMinutes"] = a.IdleBurnMinutes
		}
		if a.ErrorLoopThreshold > 0 {
			anomalyCfg["errorLoopThreshold"] = a.ErrorLoopThreshold
		}
		if a.TokenInflationMultiplier > 0 {
			anomalyCfg["tokenInflationMultiplier"] = a.TokenInflationMultiplier
		}
		if a.CheckIntervalSeconds > 0 {
			anomalyCfg["checkIntervalSeconds"] = a.CheckIntervalSeconds
		}
	}
	cfg["anomaly"] = anomalyCfg

	// --- Routing: proxy all traffic through ClawGateway ---
	evaluators := []map[string]any{}

	if gateway != nil {
		// Map CRD evaluators to observeclaw evaluator config.
		for _, ev := range gateway.Spec.Routing.Evaluators {
			entry := map[string]any{
				"name":     ev.Name,
				"type":     ev.Type,
				"priority": ev.Priority,
				"enabled":  true,
			}
			if ev.Action != "" {
				entry["action"] = ev.Action
			}
			if len(ev.Patterns) > 0 {
				entry["patterns"] = ev.Patterns
			}
			if ev.BlockReply != "" {
				entry["blockReply"] = ev.BlockReply
			}
			if ev.EmitEvent {
				entry["emitEvent"] = true
			}
			if ev.ClassifierModel != "" {
				entry["classifierModel"] = ev.ClassifierModel
			}
			if ev.TimeoutMs > 0 {
				entry["timeoutMs"] = ev.TimeoutMs
			}
			if ev.RedactReplacement != "" {
				entry["redactReplacement"] = ev.RedactReplacement
			}
			if ev.ProxyURL != "" {
				entry["proxyUrl"] = ev.ProxyURL
			}
			if ev.Routes != nil {
				routes := map[string]any{}
				for k, v := range ev.Routes {
					routes[k] = map[string]any{
						"provider": v.Provider,
						"model":    v.Model,
					}
				}
				entry["routes"] = routes
			}
			evaluators = append(evaluators, entry)
		}
	}

	// Catch-all proxy: route all LLM traffic through the ClawGateway.
	// Uses a regex that matches everything, with proxy action routing
	// to the gateway-anthropic provider (baseUrl = gateway).
	if gatewayURL != "" {
		evaluators = append(evaluators, map[string]any{
			"name":          "gateway-proxy",
			"type":          "regex",
			"priority":      0,
			"enabled":       true,
			"action":        "proxy",
			"patterns":      []string{"[\\s\\S]"},
			"proxyProvider": "gateway-anthropic",
			"proxyModel":    "claude-sonnet-4-6",
		})
	}

	cfg["routing"] = map[string]any{
		"enabled":    len(evaluators) > 0,
		"logRouting": gateway != nil && gateway.Spec.Routing.LogEveryDecision,
		"evaluators": evaluators,
	}

	// --- Webhooks from ClawGateway ---
	webhooks := []map[string]any{}
	if gateway != nil {
		for _, wh := range gateway.Spec.Webhooks {
			entry := map[string]any{
				"url": wh.URL,
			}
			if wh.MinSeverity != "" {
				entry["minSeverity"] = wh.MinSeverity
			}
			if len(wh.Headers) > 0 {
				entry["headers"] = wh.Headers
			}
			webhooks = append(webhooks, entry)
		}
	}
	cfg["webhooks"] = webhooks

	return cfg
}

// heartbeatMD generates a lightweight HEARTBEAT.md checklist for the agent.
func (r *ClawAgentReconciler) heartbeatMD(name string) string {
	return fmt.Sprintf(`# Heartbeat — %s

Check the following and respond HEARTBEAT_OK if everything is normal.
Only raise an alert if something needs attention.

- Am I responsive?
- Are my tools accessible?
- Is my workspace intact?
`, name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Named("clawagent").
		Complete(r)
}
