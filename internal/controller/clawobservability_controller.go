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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

// ClawObservabilityReconciler reconciles a ClawObservability object
type ClawObservabilityReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *ClawObservabilityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ClawObservability instance.
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

// ensureResource creates the given object if it doesn't already exist.
func (r *ClawObservabilityReconciler) ensureResource(ctx context.Context, owner *clawv1.ClawObservability, obj client.Object, desc string) error {
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

// isDeploymentAvailable checks whether a Deployment has at least one available replica.
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

// ---------------------------------------------------------------------------
// Tempo helpers
// ---------------------------------------------------------------------------

func (r *ClawObservabilityReconciler) tempoDeployment(obs *clawv1.ClawObservability, ns string) *appsv1.Deployment {
	labels := map[string]string{"app": "tempo", "app.kubernetes.io/managed-by": "clawbernetes"}
	replicas := int32(1)

	args := []string{
		"-config.file=/etc/tempo/tempo.yaml",
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tempo",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "tempo",
							Image: "grafana/tempo:latest",
							Args:  args,
							Ports: []corev1.ContainerPort{
								{Name: "otlp-http", ContainerPort: 4318, Protocol: corev1.ProtocolTCP},
								{Name: "tempo-http", ContainerPort: 3200, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			},
		},
	}
}

func (r *ClawObservabilityReconciler) tempoService(ns string) *corev1.Service {
	labels := map[string]string{"app": "tempo", "app.kubernetes.io/managed-by": "clawbernetes"}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tempo",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "otlp-http", Port: 4318, TargetPort: intstr.FromInt(4318), Protocol: corev1.ProtocolTCP},
				{Name: "tempo-http", Port: 3200, TargetPort: intstr.FromInt(3200), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Grafana helpers
// ---------------------------------------------------------------------------

func (r *ClawObservabilityReconciler) grafanaDatasourceConfigMap(obs *clawv1.ClawObservability, ns string) *corev1.ConfigMap {
	endpoint := obs.Spec.OTLPEndpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("http://tempo.%s.svc.cluster.local:3200", ns)
	}

	datasourceYAML := fmt.Sprintf(`apiVersion: 1
datasources:
  - name: Tempo
    type: tempo
    access: proxy
    url: %s
    isDefault: true
    editable: false
`, endpoint)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grafana-datasources",
			Namespace: ns,
			Labels:    map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"},
		},
		Data: map[string]string{
			"datasources.yaml": datasourceYAML,
		},
	}
}

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
			Name:      "grafana-dashboards",
			Namespace: ns,
			Labels:    map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"},
		},
		Data: map[string]string{
			"dashboards.yaml": providerYAML,
		},
	}
}

func (r *ClawObservabilityReconciler) grafanaDeployment(ns string) *appsv1.Deployment {
	labels := map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"}
	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grafana",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "grafana",
							Image: "grafana/grafana:latest",
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 3000, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "GF_AUTH_ANONYMOUS_ENABLED", Value: "true"},
								{Name: "GF_AUTH_ANONYMOUS_ORG_ROLE", Value: "Admin"},
								{Name: "GF_AUTH_DISABLE_LOGIN_FORM", Value: "true"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "datasources", MountPath: "/etc/grafana/provisioning/datasources", ReadOnly: true},
								{Name: "dashboard-provider", MountPath: "/etc/grafana/provisioning/dashboards", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "datasources",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-datasources"},
								},
							},
						},
						{
							Name: "dashboard-provider",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-dashboards"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *ClawObservabilityReconciler) grafanaService(ns string) *corev1.Service {
	labels := map[string]string{"app": "grafana", "app.kubernetes.io/managed-by": "clawbernetes"}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grafana",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 3000, TargetPort: intstr.FromInt(3000), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawObservabilityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawObservability{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("clawobservability").
		Complete(r)
}
