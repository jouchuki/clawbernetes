# Deploy a Persistent Fleet of Lobsters

This guide walks you through deploying a full Clawbernetes fleet — 5 specialized AI agents with persistent state, Telegram delivery, and A2A communication. Each agent remembers everything across restarts.

## Prerequisites

- Docker + kind + kubectl + Go 1.25+
- An OpenAI API key (or Anthropic)
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))

## 1. Create the cluster and build images

```bash
git clone https://github.com/jouchuki/clawbernetes.git
cd clawbernetes

# Create kind cluster + install CRDs + build OpenClaw image with all plugins
make demo-up
```

This builds the OpenClaw container image with observeclaw + a2a-gateway plugins baked in.

## 2. Create secrets

```bash
# LLM provider key
kubectl create secret generic openclaw-api-keys \
  --from-literal=OPENAI_API_KEY=sk-your-key-here \
  -n clawbernetes

# Telegram bot token
kubectl create secret generic fleet-telegram \
  --from-literal=TELEGRAM_BOT_TOKEN=your-bot-token \
  -n clawbernetes

# A2A tokens — one per agent
for agent in infra data security frontend devops; do
  kubectl create secret generic ${agent}-a2a \
    --from-literal=A2A_TOKEN=$(openssl rand -hex 24) \
    -n clawbernetes
done
```

## 3. Create the Telegram channel

```yaml
# fleet-channel.yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawChannel
metadata:
  name: fleet-telegram
  namespace: clawbernetes
spec:
  type: telegram
  credentialsSecret: fleet-telegram
  config:
    dmPolicy: "pairing"
    groupPolicy: "open"
    streaming: "partial"
```

```bash
kubectl apply -f fleet-channel.yaml
```

## 4. Deploy the fleet

Save this as `fleet.yaml`:

```yaml
# --- Infrastructure Agent ---
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: infra
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are the Infrastructure Lobster. You specialize in Kubernetes,
      cloud architecture, networking, and system design. You think in
      diagrams and always consider failure modes.
    agentIdentity: "Infra Lobster — K8s & cloud architect"
  model:
    provider: openai
    name: gpt-4.1
  channels: [fleet-telegram]
  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain
  a2a:
    enabled: true
    agentCardName: "Infra Lobster"
    skills: [kubernetes, networking, architecture, troubleshooting]
    securityTokenSecret: infra-a2a
    peers:
      - name: data
        agentCardUrl: http://data.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: data-a2a
      - name: security
        agentCardUrl: http://security.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: security-a2a
      - name: frontend
        agentCardUrl: http://frontend.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: frontend-a2a
      - name: devops
        agentCardUrl: http://devops.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: devops-a2a
---
# --- Data Agent ---
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: data
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are the Data Lobster. You specialize in analytics, databases,
      SQL, data pipelines, and statistical analysis. You always back
      claims with numbers.
    agentIdentity: "Data Lobster — analytics & pipelines"
  model:
    provider: openai
    name: gpt-4.1
  channels: [fleet-telegram]
  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain
  a2a:
    enabled: true
    agentCardName: "Data Lobster"
    skills: [analytics, databases, sql, pipelines, statistics]
    securityTokenSecret: data-a2a
    peers:
      - name: infra
        agentCardUrl: http://infra.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: infra-a2a
      - name: security
        agentCardUrl: http://security.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: security-a2a
      - name: frontend
        agentCardUrl: http://frontend.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: frontend-a2a
      - name: devops
        agentCardUrl: http://devops.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: devops-a2a
---
# --- Security Agent ---
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: security
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are the Security Lobster. You specialize in cybersecurity,
      compliance, threat modeling, RBAC, and zero-trust architecture.
      You are paranoid by design and always assume breach.
    agentIdentity: "Security Lobster — cybersec & compliance"
  model:
    provider: openai
    name: gpt-4.1
  channels: [fleet-telegram]
  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain
  a2a:
    enabled: true
    agentCardName: "Security Lobster"
    skills: [cybersecurity, compliance, threat-modeling, rbac, zero-trust]
    securityTokenSecret: security-a2a
    peers:
      - name: infra
        agentCardUrl: http://infra.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: infra-a2a
      - name: data
        agentCardUrl: http://data.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: data-a2a
      - name: frontend
        agentCardUrl: http://frontend.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: frontend-a2a
      - name: devops
        agentCardUrl: http://devops.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: devops-a2a
---
# --- Frontend Agent ---
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: frontend
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are the Frontend Lobster. You specialize in React, TypeScript,
      UX design, performance optimization, and accessibility. You care
      deeply about user experience.
    agentIdentity: "Frontend Lobster — React & UX specialist"
  model:
    provider: openai
    name: gpt-4.1
  channels: [fleet-telegram]
  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain
  a2a:
    enabled: true
    agentCardName: "Frontend Lobster"
    skills: [react, typescript, ux, performance, accessibility]
    securityTokenSecret: frontend-a2a
    peers:
      - name: infra
        agentCardUrl: http://infra.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: infra-a2a
      - name: data
        agentCardUrl: http://data.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: data-a2a
      - name: security
        agentCardUrl: http://security.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: security-a2a
      - name: devops
        agentCardUrl: http://devops.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: devops-a2a
---
# --- DevOps Agent ---
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: devops
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are the DevOps Lobster. You specialize in CI/CD, GitOps,
      Docker, Terraform, monitoring, and incident response. You
      automate everything and distrust manual processes.
    agentIdentity: "DevOps Lobster — CI/CD & GitOps"
  model:
    provider: openai
    name: gpt-4.1
  channels: [fleet-telegram]
  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain
  a2a:
    enabled: true
    agentCardName: "DevOps Lobster"
    skills: [cicd, gitops, docker, terraform, monitoring, incident-response]
    securityTokenSecret: devops-a2a
    peers:
      - name: infra
        agentCardUrl: http://infra.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: infra-a2a
      - name: data
        agentCardUrl: http://data.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: data-a2a
      - name: security
        agentCardUrl: http://security.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: security-a2a
      - name: frontend
        agentCardUrl: http://frontend.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: frontend-a2a
```

```bash
kubectl apply -f fleet.yaml
```

## 5. Verify

```bash
# All 5 lobsters running
kubectl get clawagents -n clawbernetes
# NAME       PHASE     POD
# infra      Running   infra (replicas: 1)
# data       Running   data (replicas: 1)
# security   Running   security (replicas: 1)
# frontend   Running   frontend (replicas: 1)
# devops     Running   devops (replicas: 1)

# PVCs created (persistent state)
kubectl get pvc -n clawbernetes
# NAME            STATUS   CAPACITY
# infra-home      Bound    5Gi
# data-home       Bound    5Gi
# security-home   Bound    5Gi
# frontend-home   Bound    5Gi
# devops-home     Bound    5Gi

# Agent Cards served (A2A discovery)
kubectl exec -n clawbernetes deploy/infra -c openclaw -- \
  curl -s http://localhost:18800/.well-known/agent-card.json | python3 -m json.tool
```

## 6. Talk to your lobsters

### Via Telegram

Message your bot on Telegram. It will ask you to pair — approve with:

```bash
kubectl exec -n clawbernetes deploy/infra -c openclaw -- \
  openclaw pairing approve telegram <PAIRING_CODE>
```

### Agent-to-Agent

From any agent, send an A2A message to another:

```bash
# Infra asks Data for analysis
kubectl exec -n clawbernetes deploy/infra -c openclaw -- \
  node /home/node/.openclaw/workspace/plugins/a2a-gateway/skill/scripts/a2a-send.mjs \
  --peer-url http://data.clawbernetes.svc.cluster.local:18800 \
  --token "$(kubectl get secret data-a2a -n clawbernetes -o jsonpath='{.data.A2A_TOKEN}' | base64 -d)" \
  --non-blocking --wait \
  --message "What's the optimal connection pool size for 100 microservices hitting Postgres?"
```

### Parallel fan-out

Ask all agents simultaneously:

```bash
for agent in data security frontend devops; do
  TOKEN=$(kubectl get secret ${agent}-a2a -n clawbernetes -o jsonpath='{.data.A2A_TOKEN}' | base64 -d)
  kubectl exec -n clawbernetes deploy/infra -c openclaw -- \
    node /home/node/.openclaw/workspace/plugins/a2a-gateway/skill/scripts/a2a-send.mjs \
    --peer-url http://${agent}.clawbernetes.svc.cluster.local:18800 \
    --token "$TOKEN" --non-blocking --wait --timeout-ms 30000 \
    --message "We're launching 10 new microservices. What should I know from your domain?" &
done
wait
```

## 7. State survives everything

Delete an agent and recreate it — the PVC is retained:

```bash
kubectl delete clawagent infra -n clawbernetes
# PVC infra-home still exists

kubectl apply -f fleet.yaml
# Infra lobster comes back with all its memory, cron jobs, and delivery queue intact
```

## 8. Scale down / tear down

```bash
# Stop all agents (PVCs stay)
kubectl delete clawagents --all -n clawbernetes

# Full teardown (deletes everything including PVCs)
make demo-down
make kind-teardown
```
