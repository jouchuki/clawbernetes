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
// This is the PII redaction proxy + optional HF classifier that the
// observeclaw plugin proxies all LLM traffic through.
func gatewayServerScript() string {
	return `"""
ObserveClaw Server — PII redaction proxy + optional local HF classifier.

Endpoints:
  POST /v1/messages          — PII-redacting proxy (strips PII, forwards to upstream)
  POST /v1/chat/completions  — Local HuggingFace classifier (optional)
  POST /config/patterns      — Push PII patterns from plugin config
  GET  /health               — Health check

The proxy reads the API key from the ANTHROPIC_API_KEY environment variable
and injects it server-side. Agents never see or send the key.
"""
import argparse
import os
import re
import time
import threading

import httpx
import uvicorn
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel

app = FastAPI(title="ObserveClaw Server")

UPSTREAM_BASE = os.environ.get("UPSTREAM_BASE_URL", "https://api.anthropic.com")

_patterns_lock = threading.Lock()
_pii_patterns: list[tuple[re.Pattern, str]] = []


def _compile_patterns(raw: list[dict]) -> list[tuple[re.Pattern, str]]:
    compiled = []
    for entry in raw:
        try:
            compiled.append((re.compile(entry["pattern"]), entry.get("replacement", "[REDACTED]")))
        except re.error as e:
            print(f"[config] bad pattern {entry.get('pattern')!r}: {e}")
    return compiled


class PatternEntry(BaseModel):
    pattern: str
    replacement: str = "[REDACTED]"


class PatternsConfig(BaseModel):
    patterns: list[PatternEntry]


@app.post("/config/patterns")
async def update_patterns(config: PatternsConfig):
    global _pii_patterns
    raw = [p.model_dump() for p in config.patterns]
    compiled = _compile_patterns(raw)
    with _patterns_lock:
        _pii_patterns = compiled
    print(f"[config] loaded {len(compiled)} PII pattern(s)")
    for p, r in compiled:
        print(f"  {p.pattern} -> {r}")
    return {"status": "ok", "patterns": len(compiled)}


def redact(text: str) -> tuple[str, list[dict]]:
    with _patterns_lock:
        patterns = list(_pii_patterns)
    redactions = []
    for pattern, replacement in patterns:
        for match in pattern.finditer(text):
            redactions.append({
                "original": match.group(),
                "replacement": replacement,
                "pattern": pattern.pattern,
            })
        text = pattern.sub(replacement, text)
    return text, redactions


def redact_messages(messages: list[dict]) -> tuple[list[dict], list[dict]]:
    all_redactions = []
    cleaned = []
    for msg in messages:
        if msg.get("role") == "user":
            content = msg.get("content", "")
            if isinstance(content, str):
                redacted, redactions = redact(content)
                all_redactions.extend(redactions)
                cleaned.append({**msg, "content": redacted})
            elif isinstance(content, list):
                new_blocks = []
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "text":
                        redacted, redactions = redact(block.get("text", ""))
                        all_redactions.extend(redactions)
                        new_blocks.append({**block, "text": redacted})
                    else:
                        new_blocks.append(block)
                cleaned.append({**msg, "content": new_blocks})
            else:
                cleaned.append(msg)
        else:
            cleaned.append(msg)
    return cleaned, all_redactions


_FORWARD_HEADERS = ("anthropic-version", "authorization", "anthropic-beta")

# Server-side API key injection -- the agent never sees or sends the key.
_SERVER_API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")


@app.api_route("/v1/messages", methods=["POST"])
async def proxy_messages(request: Request):
    body = await request.json()

    original_messages = body.get("messages", [])
    cleaned_messages, redactions = redact_messages(original_messages)
    body["messages"] = cleaned_messages

    if redactions:
        print(f"[redact] {len(redactions)} PII match(es):")
        for r in redactions:
            print(f"  {r['original']} -> {r['replacement']}")

    headers = {"Content-Type": "application/json"}
    if _SERVER_API_KEY:
        headers["x-api-key"] = _SERVER_API_KEY
    for key in _FORWARD_HEADERS:
        val = request.headers.get(key)
        if val:
            headers[key] = val

    is_stream = body.get("stream", False)

    if is_stream:
        stream_headers = {**headers, "Accept-Encoding": "identity"}
        client = httpx.AsyncClient(timeout=120.0)
        req = client.build_request(
            "POST",
            f"{UPSTREAM_BASE}/v1/messages",
            json=body,
            headers=stream_headers,
        )
        resp = await client.send(req, stream=True)

        response_headers = {}
        for key in ("content-type", "x-request-id", "content-encoding"):
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

        return StreamingResponse(
            passthrough(),
            status_code=resp.status_code,
            headers=response_headers,
        )
    else:
        async with httpx.AsyncClient(timeout=120.0) as client:
            resp = await client.post(
                f"{UPSTREAM_BASE}/v1/messages",
                json=body,
                headers=headers,
            )
            return resp.json()


_classifier = None


def _load_classifier(model_id: str, labels: list[str]) -> dict:
    import torch
    from transformers import AutoModelForSequenceClassification, AutoTokenizer

    print(f"Loading classifier: {model_id}...")
    t = time.time()
    tokenizer = AutoTokenizer.from_pretrained(model_id)
    model = AutoModelForSequenceClassification.from_pretrained(model_id)
    print(f"Classifier loaded in {time.time() - t:.1f}s ({model_id}, labels: {labels})")
    return {"tokenizer": tokenizer, "model": model, "labels": labels, "model_id": model_id}


class ClassifierMessage(BaseModel):
    role: str
    content: str


class ClassifierRequest(BaseModel):
    model: str = ""
    messages: list[ClassifierMessage]
    max_tokens: int = 50
    temperature: float = 0


@app.post("/v1/chat/completions")
async def chat_completions(req: ClassifierRequest):
    import torch

    if _classifier is None:
        return JSONResponse(
            status_code=503,
            content={"error": "Classifier not loaded. Start server without --no-classifier."},
        )

    tokenizer = _classifier["tokenizer"]
    model = _classifier["model"]
    labels = _classifier["labels"]

    text = req.messages[-1].content if req.messages else ""

    start = time.time()
    inputs = tokenizer(text, return_tensors="pt", truncation=True, max_length=512)
    with torch.no_grad():
        outputs = model(**inputs)
    probs = torch.softmax(outputs.logits, dim=1)[0].tolist()
    prediction = torch.argmax(outputs.logits, dim=1).item()
    label = labels[prediction] if prediction < len(labels) else f"class_{prediction}"
    elapsed_ms = (time.time() - start) * 1000

    prob_str = " ".join(f"{labels[i] if i < len(labels) else f'c{i}'}:{p:.2f}" for i, p in enumerate(probs))
    preview = text[:80] + "..." if len(text) > 80 else text
    print(f"[classify] [{label}] {prob_str} ({elapsed_ms:.0f}ms) | {preview}")

    return {
        "id": "cmpl-classifier",
        "object": "chat.completion",
        "model": req.model or _classifier["model_id"],
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": label},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": len(inputs["input_ids"][0]),
            "completion_tokens": 1,
            "total_tokens": len(inputs["input_ids"][0]) + 1,
        },
    }


@app.get("/health")
async def health():
    with _patterns_lock:
        pattern_count = len(_pii_patterns)
    result: dict = {
        "status": "ok",
        "services": {
            "redaction_proxy": {
                "pii_patterns": pattern_count,
                "upstream": UPSTREAM_BASE,
            },
        },
    }
    if _classifier is not None:
        result["services"]["classifier"] = {
            "model": _classifier["model_id"],
            "labels": _classifier["labels"],
        }
    else:
        result["services"]["classifier"] = {"status": "disabled"}
    return result


DEFAULT_CLASSIFIER_MODEL = "Shaheer001/Query-Complexity-Classifier"
DEFAULT_CLASSIFIER_LABELS = "simple,medium,complex"

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="ObserveClaw Server")
    parser.add_argument("--port", type=int, default=int(os.environ.get("GATEWAY_PORT", "8443")))
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument(
        "--upstream",
        default=os.environ.get("UPSTREAM_BASE_URL", "https://api.anthropic.com"),
        help="Upstream LLM base URL",
    )
    parser.add_argument(
        "--classifier-model",
        default=os.environ.get("CLASSIFIER_MODEL", DEFAULT_CLASSIFIER_MODEL),
        help=f"HuggingFace model ID (default: {DEFAULT_CLASSIFIER_MODEL})",
    )
    parser.add_argument(
        "--classifier-labels",
        default=os.environ.get("CLASSIFIER_LABELS", DEFAULT_CLASSIFIER_LABELS),
        help=f"Comma-separated labels (default: {DEFAULT_CLASSIFIER_LABELS})",
    )
    parser.add_argument(
        "--no-classifier",
        action="store_true",
        help="Skip classifier model loading (PII proxy only, fast startup)",
    )
    args = parser.parse_args()

    UPSTREAM_BASE = args.upstream

    if not args.no_classifier:
        labels = [l.strip() for l in args.classifier_labels.split(",")]
        _classifier = _load_classifier(args.classifier_model, labels)

    print(f"ObserveClaw Server listening on http://{args.host}:{args.port}")
    print(f"  /v1/messages         -> PII redaction proxy ({0} patterns) -> {UPSTREAM_BASE}")
    print(f"  /config/patterns     -> push PII patterns from plugin")
    if _classifier:
        print(f"  /v1/chat/completions -> classifier ({_classifier['model_id']})")
    else:
        print(f"  /v1/chat/completions -> disabled")
    uvicorn.run(app, host=args.host, port=args.port, log_level="warning")
`
}
