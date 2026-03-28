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
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

const openclawImage = "clawbernetes/openclaw:latest"

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
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

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

	// --- Agent Deployment ---
	dep := r.agentDeployment(agent, ns, name, otlpEndpoint, skills)
	if err := r.ensureResource(ctx, agent, dep, "agent-deployment"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Status ---
	phase, podName := r.resolveAgentStatus(ctx, ns, name)
	agent.Status.Phase = phase
	agent.Status.PodName = podName
	if err := r.Status().Update(ctx, agent); err != nil {
		log.Error(err, "unable to update ClawAgent status")
		return ctrl.Result{}, err
	}

	log.Info("reconciled ClawAgent", "name", name, "phase", phase)
	return ctrl.Result{}, nil
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
// Agent Deployment
// ---------------------------------------------------------------------------

func (r *ClawAgentReconciler) agentDeployment(agent *clawv1.ClawAgent, ns, name, otlpEndpoint string, skills []clawv1.SkillEntry) *appsv1.Deployment {
	labels := agentLabels(name)
	replicas := int32(1)

	// --- Init container: seed workspace files from the identity ConfigMap ---
	initContainer := corev1.Container{
		Name:  "seed-workspace",
		Image: "busybox:1.36",
		Command: []string{"sh", "-c", strings.Join([]string{
			"mkdir -p /workspace/skills",
			// Copy identity files if they exist in the ConfigMap mount.
			"cp /identity-src/SOUL.md /workspace/SOUL.md 2>/dev/null || true",
			"cp /identity-src/USER.md /workspace/USER.md 2>/dev/null || true",
			"cp /identity-src/IDENTITY.md /workspace/IDENTITY.md 2>/dev/null || true",
			// Copy each skill into its own directory.
			"for f in /skills-src/*; do [ -f \"$f\" ] && skill=$(basename \"$f\") && mkdir -p /workspace/skills/$skill && cp \"$f\" /workspace/skills/$skill/SKILL.md; done",
			"echo 'workspace seeded'",
		}, " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "identity-src", MountPath: "/identity-src", ReadOnly: true},
			{Name: "skills-src", MountPath: "/skills-src", ReadOnly: true},
			{Name: "workspace", MountPath: "/workspace"},
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
		Name:  "openclaw",
		Image: openclawImage,
		Env:   env,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: "/root/.openclaw/workspace"},
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
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
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
					RestartPolicy:  restartPolicy,
					InitContainers: []corev1.Container{initContainer},
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

func boolPtr(b bool) *bool {
	return &b
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Named("clawagent").
		Complete(r)
}
