// Kubernetes-style metadata (simplified for dashboard use)
export interface ObjectMeta {
  name: string
  namespace: string
  labels?: Record<string, string>
  creationTimestamp?: string
}

// --- ClawAgent ---

export interface ModelFallbackSpec {
  provider?: string
  name?: string
}

export interface AgentModelSpec {
  provider?: string
  name?: string
  fallback?: ModelFallbackSpec
}

export interface AgentIdentitySpec {
  soul?: string
  user?: string
  agentIdentity?: string
}

export interface TelemetryCaptureSpec {
  inputMessages?: boolean
  outputMessages?: boolean
  systemInstructions?: boolean
  toolDefinitions?: boolean
  toolContent?: boolean
  sampleRate?: string
}

export interface AgentLifecycleSpec {
  restartPolicy?: string
  hibernateAfterIdleMinutes?: number
  maxRuntime?: string
}

export interface WorkspaceSpec {
  mode?: string
  storageSize?: string
  storageClassName?: string
  reclaimPolicy?: string
}

export interface A2APeer {
  name: string
  agentCardUrl: string
  credentialsSecret?: string
}

export interface A2ASpec {
  enabled?: boolean
  agentCardName?: string
  agentCardDescription?: string
  skills?: string[]
  port?: number
  peers?: A2APeer[]
  securityTokenSecret?: string
}

export interface HarnessSpec {
  type?: string
  image?: string
}

export interface ClawAgentSpec {
  harness?: HarnessSpec
  identity?: AgentIdentitySpec
  skillSet?: string
  policy?: string
  channels?: string[]
  gateway?: string
  observability?: string
  telemetryCapture?: TelemetryCaptureSpec
  model?: AgentModelSpec
  resources?: {
    requests?: Record<string, string>
    limits?: Record<string, string>
  }
  lifecycle?: AgentLifecycleSpec
  workspace?: WorkspaceSpec
  credentialsSecret?: string
  a2a?: A2ASpec
}

export interface ClawAgentStatus {
  phase?: string
  podName?: string
  workspacePVC?: string
  conditions?: Array<{
    type: string
    status: string
    reason?: string
    message?: string
    lastTransitionTime?: string
  }>
}

export interface ClawAgent {
  metadata: ObjectMeta
  spec: ClawAgentSpec
  status?: ClawAgentStatus
}

// --- ClawChannel ---

export interface ClawChannelSpec {
  type: string
  enabled?: boolean
  credentialsSecret: string
  config?: Record<string, string>
}

export interface ClawChannel {
  metadata: ObjectMeta
  spec: ClawChannelSpec
  status?: { conditions?: Array<{ type: string; status: string }> }
}

// --- ClawPolicy ---

export interface ToolPolicySpec {
  allow?: string[]
  deny?: string[]
}

export interface BudgetSpec {
  daily?: number
  monthly?: number
  warnAt?: string
  downgradeModel?: string
  downgradeProvider?: string
}

export interface ClawPolicySpec {
  toolPolicy?: ToolPolicySpec
  budget?: BudgetSpec
}

export interface ClawPolicy {
  metadata: ObjectMeta
  spec: ClawPolicySpec
  status?: { conditions?: Array<{ type: string; status: string }> }
}

// --- ClawSkillSet ---

export interface SkillEntry {
  name: string
  content: string
}

export interface ClawSkillSetSpec {
  skills?: SkillEntry[]
}

export interface ClawSkillSet {
  metadata: ObjectMeta
  spec: ClawSkillSetSpec
  status?: { conditions?: Array<{ type: string; status: string }> }
}

// --- ClawGateway ---

export interface GatewayWebhookSpec {
  url?: string
  minSeverity?: string
  headers?: Record<string, string>
}

export interface EvaluatorRouteSpec {
  provider?: string
  model?: string
}

export interface GatewayEvaluatorSpec {
  name?: string
  type?: string
  priority?: number
  patterns?: string[]
  action?: string
  blockReply?: string
  emitEvent?: boolean
  classifierModel?: string
  ollamaEndpoint?: string
  timeoutMs?: number
  proxyUrl?: string
  redactReplacement?: string
  webhooks?: GatewayWebhookSpec[]
  classifierEndpoint?: string
  routes?: Record<string, EvaluatorRouteSpec>
}

export interface GatewayRoutingSpec {
  enabled?: boolean
  logEveryDecision?: boolean
  evaluators?: GatewayEvaluatorSpec[]
}

export interface GatewayAnomalySpec {
  spendSpikeMultiplier?: number
  idleBurnMinutes?: number
  errorLoopThreshold?: number
  tokenInflationMultiplier?: number
  checkIntervalSeconds?: number
}

export interface ClawGatewaySpec {
  topology?: string
  port?: number
  routing?: GatewayRoutingSpec
  anomaly?: GatewayAnomalySpec
  webhooks?: GatewayWebhookSpec[]
}

export interface ClawGateway {
  metadata: ObjectMeta
  spec: ClawGatewaySpec
  status?: { conditions?: Array<{ type: string; status: string }> }
}

// --- ClawObservability ---

export interface TempoStorageSpec {
  size?: string
  storageClass?: string
}

export interface TempoSpec {
  enabled?: boolean
  retentionDays?: number
  storage?: TempoStorageSpec
}

export interface GrafanaSpec {
  enabled?: boolean
  dashboards?: string[]
  adminCredentialsSecret?: string
  expose?: string
}

export interface ClawObservabilitySpec {
  tempo?: TempoSpec
  grafana?: GrafanaSpec
  otlpEndpoint?: string
  otlpProtocol?: string
}

export interface ClawObservability {
  metadata: ObjectMeta
  spec: ClawObservabilitySpec
  status?: {
    tempoReady?: boolean
    grafanaReady?: boolean
    conditions?: Array<{ type: string; status: string }>
  }
}

// --- API response types ---

export interface FleetSummary {
  totalAgents: number
  runningAgents: number
  totalChannels: number
  a2aConnections: number
}

export interface ActivityEvent {
  ts: string
  type: string
  agent: string
  message: string
  status?: string
  taskId?: string
  durationMs?: number
}
