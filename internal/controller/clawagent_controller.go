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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
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
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clawv1 "github.com/clawbernetes/operator/api/v1"
	"github.com/clawbernetes/operator/internal/harness"
)

// errRequeueNeeded signals the reconciler to requeue after a short delay.
var errRequeueNeeded = errors.New("requeue needed")

// ClawAgentReconciler reconciles a ClawAgent object
type ClawAgentReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	lastDashboardHash string
	dashboardMu       sync.Mutex
}

// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawskillsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawchannels,verbs=get;list;watch
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
	h := harness.ForType(agent.Spec.Harness.Type)

	// Capture current status so we can skip the write if nothing changed.
	oldPhase := agent.Status.Phase
	oldPodName := agent.Status.PodName
	oldPVC := agent.Status.WorkspacePVC

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

	// --- Resolve ClawChannels if referenced ---
	var channels []clawv1.ClawChannel
	for _, chName := range agent.Spec.Channels {
		ch := &clawv1.ClawChannel{}
		chKey := types.NamespacedName{Name: chName, Namespace: ns}
		if err := r.Get(ctx, chKey, ch); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("referenced ClawChannel not found", "name", chName)
		} else if ch.Spec.IsEnabled() {
			channels = append(channels, *ch)
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

	// --- Harness config ConfigMap ---
	configCM, err := r.harnessConfigMap(h, agent, ns, name, gatewayURL, otlpEndpoint, policy, gateway, channels)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureResource(ctx, agent, configCM, "harness-config-configmap"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Workspace storage ---
	var activePVC string
	if agent.Spec.Workspace.IsPersistent() {
		if err := r.ensurePVC(ctx, agent, ns, name); err != nil {
			if errors.Is(err, errRequeueNeeded) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}
		activePVC = activePVCName(agent, name)
	} else {
		if err := r.cleanupOrphanedPVC(ctx, ns, name); err != nil {
			return ctrl.Result{}, err
		}
	}

	// --- Agent Deployment ---
	dep := r.agentDeployment(deploymentParams{
		Agent:        agent,
		Namespace:    ns,
		Name:         name,
		OTLPEndpoint: otlpEndpoint,
		GatewayURL:   gatewayURL,
		ActivePVC:    activePVC,
		Channels:     channels,
		Harness:      h,
	})
	if err := r.ensureResource(ctx, agent, dep, "agent-deployment"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Agent Service ---
	svc := r.agentService(h, agent, ns, name)
	if err := r.ensureResource(ctx, agent, svc, "agent-service"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Status (skip write if unchanged) ---
	phase, podName := r.resolveAgentStatus(ctx, ns, name)
	agent.Status.Phase = phase
	agent.Status.PodName = podName
	agent.Status.WorkspacePVC = activePVC
	if phase != oldPhase || podName != oldPodName || activePVC != oldPVC {
		if err := r.Status().Update(ctx, agent); err != nil {
			log.Error(err, "unable to update ClawAgent status")
			return ctrl.Result{}, err
		}
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
			Channels:      a.Spec.Channels,
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

	// Skip the ConfigMap write if the dashboard content hasn't changed.
	sum := sha256.Sum256([]byte(html))
	hash := hex.EncodeToString(sum[:])
	r.dashboardMu.Lock()
	defer r.dashboardMu.Unlock()
	if hash == r.lastDashboardHash {
		return nil
	}

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
			r.lastDashboardHash = hash
			return r.Create(ctx, cm)
		}
		return err
	}
	existing.Data = cm.Data
	if err := r.Update(ctx, existing); err != nil {
		return err
	}
	r.lastDashboardHash = hash
	return nil
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

	// Update existing resource if spec has drifted.
	switch desired := obj.(type) {
	case *corev1.ConfigMap:
		old := existing.(*corev1.ConfigMap)
		if !reflect.DeepEqual(old.Data, desired.Data) || !reflect.DeepEqual(old.BinaryData, desired.BinaryData) {
			old.Data = desired.Data
			old.BinaryData = desired.BinaryData
			log.Info("updating resource", "kind", desc, "name", key.Name)
			return r.Update(ctx, old)
		}
	case *appsv1.Deployment:
		old := existing.(*appsv1.Deployment)
		if !reflect.DeepEqual(old.Spec, desired.Spec) {
			old.Spec = desired.Spec
			log.Info("updating resource", "kind", desc, "name", key.Name)
			return r.Update(ctx, old)
		}
	case *corev1.Service:
		old := existing.(*corev1.Service)
		if !reflect.DeepEqual(old.Spec.Ports, desired.Spec.Ports) || !reflect.DeepEqual(old.Spec.Selector, desired.Spec.Selector) {
			// Preserve ClusterIP — it is immutable and assigned by the API server.
			desired.Spec.ClusterIP = old.Spec.ClusterIP
			old.Spec = desired.Spec
			log.Info("updating resource", "kind", desc, "name", key.Name)
			return r.Update(ctx, old)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// PVC lifecycle: ensure, sync owner refs, resize, cleanup
// ---------------------------------------------------------------------------

func activePVCName(agent *clawv1.ClawAgent, name string) string {
	if agent.Status.WorkspacePVC != "" {
		return agent.Status.WorkspacePVC
	}
	return name + clawv1.PVCSuffix
}

func buildPVC(pvcName, ns, agentName string, size resource.Quantity, ws clawv1.WorkspaceSpec) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: ns,
			Labels:    agentLabels(agentName),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
	if ws.StorageClassName != nil {
		pvc.Spec.StorageClassName = ws.StorageClassName
	}
	return pvc
}

func buildMigrationPod(podName, ns, agentName, srcPVC, dstPVC string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels:    agentLabels(agentName),
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
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: srcPVC, ReadOnly: true},
				}},
				{Name: "dst", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: dstPVC},
				}},
			},
		},
	}
}

func (r *ClawAgentReconciler) ensurePVC(ctx context.Context, owner *clawv1.ClawAgent, ns, name string) error {
	log := logf.FromContext(ctx)
	pvcName := activePVCName(owner, name)
	storageSize := owner.Spec.Workspace.ResolvedStorageSize()
	reclaimPolicy := owner.Spec.Workspace.ResolvedReclaimPolicy()

	pvc := buildPVC(pvcName, ns, name, resource.MustParse(storageSize), owner.Spec.Workspace)
	if reclaimPolicy == clawv1.ReclaimPolicyDelete {
		if err := ctrl.SetControllerReference(owner, pvc, r.Scheme); err != nil {
			return fmt.Errorf("setting owner reference on PVC: %w", err)
		}
	}

	existing := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ns}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("creating PVC for persistent workspace", "pvc", pvcName, "reclaimPolicy", reclaimPolicy)
			return r.Create(ctx, pvc)
		}
		return err
	}

	// Sync owner references to match current reclaimPolicy.
	if err := r.syncPVCOwnerRefs(ctx, owner, existing, pvcName, reclaimPolicy); err != nil {
		return err
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

func (r *ClawAgentReconciler) syncPVCOwnerRefs(ctx context.Context, owner *clawv1.ClawAgent, pvc *corev1.PersistentVolumeClaim, pvcName, reclaimPolicy string) error {
	log := logf.FromContext(ctx)
	needsUpdate := false

	if reclaimPolicy == clawv1.ReclaimPolicyDelete && len(pvc.OwnerReferences) == 0 {
		if err := ctrl.SetControllerReference(owner, pvc, r.Scheme); err != nil {
			return fmt.Errorf("adding owner reference to existing PVC: %w", err)
		}
		needsUpdate = true
		log.Info("adding owner reference to PVC (reclaimPolicy changed to delete)", "pvc", pvcName)
	} else if reclaimPolicy == clawv1.ReclaimPolicyRetain && len(pvc.OwnerReferences) > 0 {
		pvc.OwnerReferences = nil
		needsUpdate = true
		log.Info("removing owner reference from PVC (reclaimPolicy changed to retain)", "pvc", pvcName)
	}

	if needsUpdate {
		return r.Update(ctx, pvc)
	}
	return nil
}

// resizePVC handles PVC expansion by creating a new PVC, migrating data via a
// one-shot pod, then deleting the old PVC and updating the deployment to use
// the new one.
//
// State machine (reentrant across reconcile loops):
//  1. Scale deployment to 0          → requeue
//  2. New PVC doesn't exist          → create it
//  3. Migration pod doesn't exist    → create it (mounts old + new)
//  4. Migration pod succeeded        → delete old PVC, delete pod, update status
//  5. Migration pod failed           → delete pod, return error
//  6. Migration pod still running    → requeue
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
	if err := r.Get(ctx, types.NamespacedName{Name: newPVCName, Namespace: ns}, &corev1.PersistentVolumeClaim{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		newPVC := buildPVC(newPVCName, ns, name, newSize, owner.Spec.Workspace)
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
		log.Info("creating migration pod", "pod", migratePodName, "from", pvcName, "to", newPVCName)
		return r.Create(ctx, buildMigrationPod(migratePodName, ns, name, pvcName, newPVCName))
	}

	// Step 4: Check migration pod status.
	switch migratePod.Status.Phase {
	case corev1.PodSucceeded:
		log.Info("migration complete — cleaning up old PVC", "oldPVC", pvcName, "newPVC", newPVCName)
		_ = r.Delete(ctx, migratePod)
		if err := r.Delete(ctx, oldPVC); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
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
// Harness config ConfigMap — config file + HEARTBEAT.md
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) harnessConfigMap(h harness.Harness, agent *clawv1.ClawAgent, ns, name, gatewayURL, otlpEndpoint string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway, channels []clawv1.ClawChannel) (*corev1.ConfigMap, error) {
	configData, err := h.BuildConfig(harness.ConfigInput{
		Agent:        agent,
		Namespace:    ns,
		Name:         name,
		GatewayURL:   gatewayURL,
		OTLPEndpoint: otlpEndpoint,
		Policy:       policy,
		Gateway:      gateway,
		Channels:     channels,
	})
	if err != nil {
		return nil, fmt.Errorf("building harness config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + h.ConfigMapSuffix(),
			Namespace: ns,
			Labels:    agentLabels(name),
		},
		Data: map[string]string{
			h.ConfigFileName(): configData,
			"HEARTBEAT.md":     r.heartbeatMD(name),
			"TOOLS.md":         r.toolsMD(agent, ns, name),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Agent Deployment
// ---------------------------------------------------------------------------

type deploymentParams struct {
	Agent        *clawv1.ClawAgent
	Namespace    string
	Name         string
	OTLPEndpoint string
	GatewayURL   string
	ActivePVC    string
	Channels     []clawv1.ClawChannel
	Harness      harness.Harness
}

func (r *ClawAgentReconciler) agentDeployment(p deploymentParams) *appsv1.Deployment {
	agent := p.Agent
	ns := p.Namespace
	name := p.Name
	otlpEndpoint := p.OTLPEndpoint
	activePVC := p.ActivePVC
	labels := agentLabels(name)
	replicas := int32(1)
	h := p.Harness

	image := p.Agent.Spec.Harness.Image
	if image == "" {
		image = h.DefaultImage()
	}
	gatewayPort := int(h.GatewayPort())

	// First init container: copy baked-in extensions from
	// the harness image into the shared emptyDir volume.
	copyExtensions := corev1.Container{
		Name:            "copy-extensions",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c", strings.Join(h.CopyExtensionsCommands(), " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "harness-home", MountPath: "/harness-home"},
		},
	}

	// Second init container: seed workspace with config, identity, and skills.
	seedWorkspace := corev1.Container{
		Name:    "seed-workspace",
		Image:   "busybox:1.36",
		Command: []string{"sh", "-c", strings.Join(h.SeedCommands(), " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config-src", MountPath: "/config-src", ReadOnly: true},
			{Name: "identity-src", MountPath: "/identity-src", ReadOnly: true},
			{Name: "skills-src", MountPath: "/skills-src", ReadOnly: true},
			{Name: "harness-home", MountPath: "/harness-home"},
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
		Name:            h.ContainerName(),
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         h.ContainerCommand(),
		Env:             env,
		Ports: []corev1.ContainerPort{
			{Name: "gateway", ContainerPort: int32(gatewayPort), Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: buildProbe(h.ReadinessPath(), gatewayPort, 5, 10),
		LivenessProbe:  buildProbe(h.LivenessPath(), gatewayPort, 10, 30),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "harness-home", MountPath: h.HomePath()},
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
		harnessHomeVolume(activePVC, agent.Spec.Workspace.Mode),
		{
			Name: "config-src",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + h.ConfigMapSuffix()},
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
			MountPath: h.HomePath() + "/credentials",
			ReadOnly:  true,
		})
	}

	// Inject secrets as env vars for ${VAR} substitution in openclaw.json.
	// Deduplicate to avoid mounting the same secret twice.
	injectedSecrets := map[string]bool{}
	injectSecret := func(name string) {
		if name == "" || injectedSecrets[name] {
			return
		}
		injectedSecrets[name] = true
		mainContainer.EnvFrom = append(mainContainer.EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Optional:             boolPtr(true),
			},
		})
	}

	// Model provider API key (direct providers only, no gateway).
	if agent.Spec.Model.Provider != "" && p.GatewayURL == "" {
		injectSecret(clawv1.DefaultProviderAPIKeysSecret)
	}

	// Channel credential secrets.
	for _, ch := range p.Channels {
		injectSecret(ch.Spec.CredentialsSecret)
	}

	// A2A gateway credentials.
	if agent.Spec.A2A.Enabled {
		// Add A2A port to the container.
		a2aPort := agent.Spec.A2A.ResolvedPort()
		mainContainer.Ports = append(mainContainer.Ports, corev1.ContainerPort{
			Name: "a2a", ContainerPort: int32(a2aPort), Protocol: corev1.ProtocolTCP,
		})
		// Inject security token secret (A2A_TOKEN key → A2A_TOKEN env var).
		injectSecret(agent.Spec.A2A.SecurityTokenSecret)
		// Inject peer credential secrets as PEER_<NAME>_TOKEN env vars.
		// Each peer secret has key A2A_TOKEN, mapped to a unique env var name.
		for _, peer := range agent.Spec.A2A.Peers {
			if peer.CredentialsSecret != "" {
				envName := fmt.Sprintf("PEER_%s_TOKEN", strings.ToUpper(strings.ReplaceAll(peer.Name, "-", "_")))
				mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{
					Name: envName,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: peer.CredentialsSecret},
							Key:                  "A2A_TOKEN",
							Optional:             boolPtr(true),
						},
					},
				})
			}
		}
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
					SecurityContext: podSecurityContext(h),
					RestartPolicy:   restartPolicy,
					InitContainers:  []corev1.Container{copyExtensions, seedWorkspace},
					Containers:      []corev1.Container{mainContainer},
					Volumes:         volumes,
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

func harnessHomeVolume(pvcName, mode string) corev1.Volume {
	if mode == clawv1.WorkspaceModePersistent && pvcName != "" {
		return corev1.Volume{
			Name: "harness-home",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		}
	}
	return corev1.Volume{
		Name: "harness-home",
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

func buildProbe(path string, port, initialDelay, period int) *corev1.Probe {
	if path == "" {
		// No HTTP health endpoint — use an exec probe that checks the process is alive.
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"test", "-f", "/proc/1/status"},
				},
			},
			InitialDelaySeconds: int32(initialDelay),
			PeriodSeconds:       int32(period),
		}
	}
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: path,
				Port: intstr.FromInt(port),
			},
		},
		InitialDelaySeconds: int32(initialDelay),
		PeriodSeconds:       int32(period),
	}
}

func podSecurityContext(h harness.Harness) *corev1.PodSecurityContext {
	uid := h.RunAsUser()
	if uid == nil {
		return nil
	}
	return &corev1.PodSecurityContext{
		RunAsUser:  uid,
		RunAsGroup: uid,
		FSGroup:    uid,
	}
}

// ---------------------------------------------------------------------------
// Agent Service
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) agentService(h harness.Harness, agent *clawv1.ClawAgent, ns, name string) *corev1.Service {
	labels := agentLabels(name)
	gatewayPort := int(h.GatewayPort())
	ports := []corev1.ServicePort{
		{Name: "gateway", Port: int32(gatewayPort), TargetPort: intstr.FromInt(gatewayPort), Protocol: corev1.ProtocolTCP},
	}
	if agent.Spec.A2A.Enabled {
		a2aPort := agent.Spec.A2A.ResolvedPort()
		ports = append(ports, corev1.ServicePort{
			Name: "a2a", Port: int32(a2aPort), TargetPort: intstr.FromInt(a2aPort), Protocol: corev1.ProtocolTCP,
		})
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    ports,
		},
	}
}

// ---------------------------------------------------------------------------
// Heartbeat & Tools
// ---------------------------------------------------------------------------

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

// toolsMD generates TOOLS.md with A2A peer information so the agent knows
// how to discover and communicate with other agents in the fleet.
func (r *ClawAgentReconciler) toolsMD(agent *clawv1.ClawAgent, ns, name string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Tools — %s\n\n", name))

	if agent.Spec.A2A.Enabled && len(agent.Spec.A2A.Peers) > 0 {
		b.WriteString("## A2A Gateway (Agent-to-Agent Communication)\n\n")
		b.WriteString("You have an A2A Gateway plugin running on port 18800. You can talk to other agents.\n\n")
		b.WriteString("### Peers\n\n")
		b.WriteString("| Peer | Agent Card URL |\n")
		b.WriteString("|------|---------------|\n")
		for _, p := range agent.Spec.A2A.Peers {
			b.WriteString(fmt.Sprintf("| %s | %s |\n", p.Name, p.AgentCardURL))
		}

		b.WriteString("\n### How to send a message to a peer\n\n")
		b.WriteString("Use the exec tool to run:\n\n")
		b.WriteString("```bash\n")
		b.WriteString("node /home/node/.openclaw/workspace/plugins/a2a-gateway/skill/scripts/a2a-send.mjs \\\n")
		b.WriteString("  --peer-url http://<PEER_NAME>." + ns + ".svc.cluster.local:18800 \\\n")
		b.WriteString("  --token \"$PEER_<PEER_NAME_UPPER>_TOKEN\" \\\n")
		b.WriteString("  --non-blocking --wait \\\n")
		b.WriteString("  --message \"YOUR MESSAGE HERE\"\n")
		b.WriteString("```\n\n")

		b.WriteString("### Quick reference for each peer\n\n")
		for _, p := range agent.Spec.A2A.Peers {
			envVar := fmt.Sprintf("PEER_%s_TOKEN", strings.ToUpper(strings.ReplaceAll(p.Name, "-", "_")))
			peerURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:18800", p.Name, ns)
			b.WriteString(fmt.Sprintf("**%s:**\n```bash\nnode /home/node/.openclaw/workspace/plugins/a2a-gateway/skill/scripts/a2a-send.mjs --peer-url %s --token \"$%s\" --non-blocking --wait --message \"your message\"\n```\n\n", p.Name, peerURL, envVar))
		}

		b.WriteString("### Tips\n\n")
		b.WriteString("- Use `--non-blocking --wait` for reliable responses\n")
		b.WriteString("- The peer token is already in your environment as `$PEER_<NAME>_TOKEN`\n")
		b.WriteString("- You can ask peers questions, delegate tasks, or coordinate work\n")
		b.WriteString("- Each peer has their own personality and expertise — check their Agent Card\n")
	}

	return b.String()
}

// SetupWithManager sets up the controller with the Manager.
// ---------------------------------------------------------------------------
// Cross-resource watch mappers
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) findAgentsReferencingField(ctx context.Context, obj client.Object, match func(clawv1.ClawAgentSpec) bool) []reconcile.Request {
	agents := &clawv1.ClawAgentList{}
	if err := r.List(ctx, agents, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, a := range agents.Items {
		if match(a.Spec) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: a.Name, Namespace: a.Namespace},
			})
		}
	}
	return requests
}

func (r *ClawAgentReconciler) findAgentsForPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.findAgentsReferencingField(ctx, obj, func(s clawv1.ClawAgentSpec) bool {
		return s.Policy == obj.GetName()
	})
}

func (r *ClawAgentReconciler) findAgentsForSkillSet(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.findAgentsReferencingField(ctx, obj, func(s clawv1.ClawAgentSpec) bool {
		return s.SkillSet == obj.GetName()
	})
}

func (r *ClawAgentReconciler) findAgentsForChannel(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.findAgentsReferencingField(ctx, obj, func(s clawv1.ClawAgentSpec) bool {
		for _, ch := range s.Channels {
			if ch == obj.GetName() {
				return true
			}
		}
		return false
	})
}

func (r *ClawAgentReconciler) findAgentsForGateway(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.findAgentsReferencingField(ctx, obj, func(s clawv1.ClawAgentSpec) bool {
		return s.Gateway == obj.GetName()
	})
}

func (r *ClawAgentReconciler) findAgentsForObservability(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.findAgentsReferencingField(ctx, obj, func(s clawv1.ClawAgentSpec) bool {
		return s.Observability == obj.GetName()
	})
}

func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Watches(&clawv1.ClawPolicy{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForPolicy)).
		Watches(&clawv1.ClawSkillSet{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForSkillSet)).
		Watches(&clawv1.ClawChannel{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForChannel)).
		Watches(&clawv1.ClawGateway{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForGateway)).
		Watches(&clawv1.ClawObservability{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForObservability)).
		WithOptions(ctrlcontroller.Options{MaxConcurrentReconciles: 5}).
		Named("clawagent").
		Complete(r)
}
