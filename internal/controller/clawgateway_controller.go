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

// ClawGatewayReconciler reconciles a ClawGateway object
type ClawGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways/finalizers,verbs=update

func (r *ClawGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	gw := &clawv1.ClawGateway{}
	if err := r.Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	ns := gw.Namespace
	name := gw.Name
	port := gw.Spec.Port
	if port == 0 {
		port = 8443
	}

	// Check if any evaluator needs Ollama (LlamaGuard).
	needsOllama := false
	ollamaModel := "llama-guard3:1b"
	for _, ev := range gw.Spec.Routing.Evaluators {
		if ev.Type == "classifier" && ev.OllamaEndpoint != "" {
			needsOllama = true
			if ev.ClassifierModel != "" {
				ollamaModel = ev.ClassifierModel
			}
			break
		}
	}

	// --- Ollama (if needed for prompt injection) ---
	if needsOllama {
		if err := r.ensureResource(ctx, gw, r.ollamaDeployment(ns, name, ollamaModel), "ollama-deployment"); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.ensureResource(ctx, gw, r.ollamaService(ns, name), "ollama-service"); err != nil {
			return ctrl.Result{}, err
		}
	}

	// --- Gateway server script ---
	if err := r.ensureResource(ctx, gw, r.gatewayScriptConfigMap(gw, ns, name), "gateway-script-cm"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Gateway Deployment ---
	ollamaEndpoint := ""
	if needsOllama {
		ollamaEndpoint = fmt.Sprintf("http://%s-ollama.%s.svc.cluster.local:11434", name, ns)
	}
	if err := r.ensureResource(ctx, gw, r.gatewayDeployment(gw, ns, name, port, ollamaEndpoint), "gateway-deployment"); err != nil {
		return ctrl.Result{}, err
	}

	// --- Gateway Service ---
	if err := r.ensureResource(ctx, gw, r.gatewayService(ns, name, port), "gateway-service"); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciled ClawGateway", "name", name, "port", port, "ollama", needsOllama)
	return ctrl.Result{}, nil
}

func (r *ClawGatewayReconciler) ensureResource(ctx context.Context, owner *clawv1.ClawGateway, obj client.Object, desc string) error {
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
// Gateway server script — embedded as a ConfigMap
// ---------------------------------------------------------------------------

func (r *ClawGatewayReconciler) gatewayScriptConfigMap(gw *clawv1.ClawGateway, ns, name string) *corev1.ConfigMap {
	// Build evaluator config for the server from CRD spec.
	serverScript := gatewayServerScript()

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-gateway-script",
			Namespace: ns,
			Labels:    gatewayLabels(name),
		},
		Data: map[string]string{
			"server.py": serverScript,
		},
	}
}

// ---------------------------------------------------------------------------
// Gateway Deployment
// ---------------------------------------------------------------------------

func (r *ClawGatewayReconciler) gatewayDeployment(gw *clawv1.ClawGateway, ns, name string, port int, ollamaEndpoint string) *appsv1.Deployment {
	labels := gatewayLabels(name)
	replicas := int32(1)

	env := []corev1.EnvVar{
		{Name: "GATEWAY_PORT", Value: fmt.Sprintf("%d", port)},
		{Name: "UPSTREAM_BASE_URL", Value: "https://api.anthropic.com"},
		{Name: "PYTHONPATH", Value: "/deps"},
	}
	if ollamaEndpoint != "" {
		env = append(env, corev1.EnvVar{Name: "OLLAMA_ENDPOINT", Value: ollamaEndpoint})
	}

	// Build routing config env vars from spec evaluators.
	for _, ev := range gw.Spec.Routing.Evaluators {
		if ev.Type == "classifier" && ev.Routes != nil {
			// Complexity router — inject model routes.
			if simple, ok := ev.Routes["simple"]; ok {
				env = append(env, corev1.EnvVar{Name: "ROUTE_SIMPLE_MODEL", Value: simple.Model})
			}
			if complex, ok := ev.Routes["complex"]; ok {
				env = append(env, corev1.EnvVar{Name: "ROUTE_COMPLEX_MODEL", Value: complex.Model})
			}
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-gateway",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "install-deps",
							Image: "python:3.11-slim",
							Command: []string{"pip", "install",
								"--target=/deps",
								"fastapi", "uvicorn[standard]", "httpx", "pydantic",
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "deps", MountPath: "/deps"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "gateway",
							Image: "python:3.11-slim",
							Command: []string{"python", "/app/server.py",
								"--port", fmt.Sprintf("%d", port),
								"--no-classifier",
							},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: int32(port), Protocol: corev1.ProtocolTCP},
							},
							Env: env,
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "openclaw-api-keys"},
										Optional:             boolPtr(true),
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "script", MountPath: "/app", ReadOnly: true},
								{Name: "deps", MountPath: "/deps", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "script",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: name + "-gateway-script"},
								},
							},
						},
						{
							Name: "deps",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Gateway Service
// ---------------------------------------------------------------------------

func (r *ClawGatewayReconciler) gatewayService(ns, name string, port int) *corev1.Service {
	labels := gatewayLabels(name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-gateway",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: int32(port), TargetPort: intstr.FromInt(port), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Ollama Deployment + Service (for LlamaGuard prompt injection)
// ---------------------------------------------------------------------------

func (r *ClawGatewayReconciler) ollamaDeployment(ns, gwName, model string) *appsv1.Deployment {
	name := gwName + "-ollama"
	labels := map[string]string{
		"app":                          name,
		"clawbernetes.io/gateway":      gwName,
		"app.kubernetes.io/managed-by": "clawbernetes",
	}
	replicas := int32(1)

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
					Containers: []corev1.Container{
						{
							Name:  "ollama",
							Image: "ollama/ollama:latest",
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 11434, Protocol: corev1.ProtocolTCP},
							},
							// Pull the model on startup via a lifecycle hook.
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c",
											fmt.Sprintf("sleep 5 && ollama pull %s", model),
										},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("4Gi"),
									corev1.ResourceCPU:    resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *ClawGatewayReconciler) ollamaService(ns, gwName string) *corev1.Service {
	name := gwName + "-ollama"
	labels := map[string]string{
		"app":                          name,
		"clawbernetes.io/gateway":      gwName,
		"app.kubernetes.io/managed-by": "clawbernetes",
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 11434, TargetPort: intstr.FromInt(11434), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func gatewayLabels(name string) map[string]string {
	return map[string]string{
		"app":                          name + "-gateway",
		"clawbernetes.io/gateway":      name,
		"app.kubernetes.io/managed-by": "clawbernetes",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1.ClawGateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("clawgateway").
		Complete(r)
}

// gatewayServerScript returns the observeclaw-server Python source.
// For hackathon MVP, this is embedded directly. In production you'd build
// a dedicated container image.
func gatewayServerScript() string {
	return `"""
ClawGateway Server — cost routing proxy + prompt injection blocking.
Deployed by the Clawbernetes operator as a ConfigMap-mounted script.
"""
import argparse
import os
import re
import time
import threading
import json

import httpx
import uvicorn
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel

app = FastAPI(title="ClawGateway Server")

UPSTREAM_BASE = os.environ.get("UPSTREAM_BASE_URL", "https://api.anthropic.com")
OLLAMA_ENDPOINT = os.environ.get("OLLAMA_ENDPOINT", "")
ROUTE_SIMPLE_MODEL = os.environ.get("ROUTE_SIMPLE_MODEL", "claude-haiku-4-5")
ROUTE_COMPLEX_MODEL = os.environ.get("ROUTE_COMPLEX_MODEL", "claude-sonnet-4-6")

# --- Prompt injection patterns (regex, zero-latency) ---
INJECTION_PATTERNS = [
    re.compile(r"\b(rm\s+-rf|sudo\s+|chmod\s+777)", re.IGNORECASE),
    re.compile(r"\b(curl\s+.*\|\s*sh|wget\s+.*\|\s*bash)", re.IGNORECASE),
    re.compile(r"ignore\s+(all\s+)?previous\s+instructions", re.IGNORECASE),
    re.compile(r"you\s+are\s+now\s+(a|an)\s+", re.IGNORECASE),
    re.compile(r"\[SYSTEM\]|\[INST\]", re.IGNORECASE),
]


def check_injection(text: str) -> bool:
    for pattern in INJECTION_PATTERNS:
        if pattern.search(text):
            return True
    return False


def extract_user_text(messages: list[dict]) -> str:
    parts = []
    for msg in messages:
        if msg.get("role") == "user":
            content = msg.get("content", "")
            if isinstance(content, str):
                parts.append(content)
            elif isinstance(content, list):
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "text":
                        parts.append(block.get("text", ""))
    return " ".join(parts)


async def classify_complexity(text: str) -> str:
    """Call Ollama or local classifier to determine query complexity."""
    if not OLLAMA_ENDPOINT:
        return "complex"
    try:
        async with httpx.AsyncClient(timeout=3.0) as client:
            resp = await client.post(
                f"{OLLAMA_ENDPOINT}/v1/chat/completions",
                json={
                    "model": "query-complexity",
                    "messages": [{"role": "user", "content": text[:500]}],
                    "max_tokens": 10,
                },
            )
            if resp.status_code == 200:
                label = resp.json()["choices"][0]["message"]["content"].strip().lower()
                if "simple" in label:
                    return "simple"
            return "complex"
    except Exception as e:
        print(f"[classify] error: {e}, defaulting to complex")
        return "complex"


_FORWARD_HEADERS = ("x-api-key", "anthropic-version", "authorization", "anthropic-beta")


@app.api_route("/v1/messages", methods=["POST"])
async def proxy_messages(request: Request):
    body = await request.json()
    messages = body.get("messages", [])
    user_text = extract_user_text(messages)

    # --- Stage 1: Prompt injection check (regex, ~0ms) ---
    if check_injection(user_text):
        print(f"[BLOCKED] prompt injection detected: {user_text[:80]}...")
        return JSONResponse(
            status_code=400,
            content={"error": {"type": "blocked", "message": "Blocked: prompt injection detected."}},
        )

    # --- Stage 2: Complexity routing ---
    complexity = await classify_complexity(user_text)
    routed_model = ROUTE_SIMPLE_MODEL if complexity == "simple" else ROUTE_COMPLEX_MODEL
    original_model = body.get("model", "unknown")
    body["model"] = routed_model
    print(f"[route] {complexity} | {original_model} -> {routed_model} | {user_text[:60]}...")

    # Forward to upstream
    headers = {"Content-Type": "application/json"}
    for key in _FORWARD_HEADERS:
        val = request.headers.get(key)
        if val:
            headers[key] = val

    is_stream = body.get("stream", False)

    if is_stream:
        client = httpx.AsyncClient(timeout=120.0)
        req = client.build_request("POST", f"{UPSTREAM_BASE}/v1/messages", json=body, headers=headers)
        resp = await client.send(req, stream=True)

        response_headers = {}
        for key in ("content-type", "x-request-id"):
            val = resp.headers.get(key)
            if val:
                response_headers[key] = val

        async def passthrough():
            try:
                async for raw in resp.aiter_raw():
                    yield raw
            finally:
                await resp.aclose()
                await client.aclose()

        return StreamingResponse(passthrough(), status_code=resp.status_code, headers=response_headers)
    else:
        async with httpx.AsyncClient(timeout=120.0) as client:
            resp = await client.post(f"{UPSTREAM_BASE}/v1/messages", json=body, headers=headers)
            return JSONResponse(content=resp.json(), status_code=resp.status_code)


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "routing": {
            "simple": ROUTE_SIMPLE_MODEL,
            "complex": ROUTE_COMPLEX_MODEL,
        },
        "upstream": UPSTREAM_BASE,
        "ollama": OLLAMA_ENDPOINT or "disabled",
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="ClawGateway Server")
    parser.add_argument("--port", type=int, default=int(os.environ.get("GATEWAY_PORT", "8443")))
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--no-classifier", action="store_true")
    args = parser.parse_args()

    print(f"ClawGateway listening on http://{args.host}:{args.port}")
    print(f"  /v1/messages -> cost routing proxy -> {UPSTREAM_BASE}")
    print(f"  simple -> {ROUTE_SIMPLE_MODEL} | complex -> {ROUTE_COMPLEX_MODEL}")
    uvicorn.run(app, host=args.host, port=args.port, log_level="warning")
`
}
