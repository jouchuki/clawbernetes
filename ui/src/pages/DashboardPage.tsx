import { useEffect, useState, useMemo } from 'react'
import { apiFetch } from '../api/client'
import type { FleetSummary, ClawAgent, ActivityEvent } from '../api/types'
import StatusBadge from '../components/shared/StatusBadge'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

// Color palette for harness types
const HARNESS_COLORS: Record<string, string> = {
  openclaw: '#4ecca3',
  observeclaw: '#e94560',
  hermes: '#ffd369',
}
const HARNESS_COLOR_DEFAULT = '#888888'

function harnessColor(type: string) {
  return HARNESS_COLORS[type] || HARNESS_COLOR_DEFAULT
}

export default function DashboardPage() {
  const [summary, setSummary] = useState<FleetSummary | null>(null)
  const [agents, setAgents] = useState<ClawAgent[]>([])
  const [activity, setActivity] = useState<ActivityEvent[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false

    async function fetchAll() {
      try {
        const [s, aRaw, act] = await Promise.all([
          apiFetch<FleetSummary>('/api/summary'),
          apiFetch<{ items?: ClawAgent[] } | ClawAgent[]>('/api/agents'),
          apiFetch<ActivityEvent[]>('/api/activity'),
        ])
        if (!cancelled) {
          setSummary(s)
          setAgents(Array.isArray(aRaw) ? aRaw : (aRaw.items || []))
          setActivity(Array.isArray(act) ? act : [])
          setError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to fetch data')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    fetchAll()
    const interval = setInterval(fetchAll, 5000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [])

  if (loading) return <LoadingSpinner />

  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold text-claw-accent">Fleet Dashboard</h1>

      {error && (
        <div className="mb-6">
          <ErrorAlert message={error} />
        </div>
      )}

      {/* Summary cards */}
      {summary && (
        <div className="mb-8 grid grid-cols-2 gap-4 lg:grid-cols-4">
          <SummaryCard label="Total Agents" value={summary.totalAgents} color="text-claw-accent" />
          <SummaryCard label="Running" value={summary.runningAgents} color="text-claw-accent" />
          <SummaryCard label="Channels" value={summary.totalChannels} color="text-claw-text" />
          <SummaryCard label="A2A Links" value={summary.a2aConnections} color="text-claw-text" />
        </div>
      )}

      {/* Fleet topology graph */}
      {agents.length > 0 && (
        <div className="mb-8">
          <FleetGraph agents={agents} />
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-3">
        {/* Agent cards */}
        <div className="xl:col-span-2">
          <h2 className="mb-4 text-lg font-semibold text-claw-accent">Agents</h2>
          {agents.length === 0 && !error ? (
            <p className="text-claw-dim">No agents found.</p>
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {agents.map((agent) => (
                <AgentCard key={agent.metadata.name} agent={agent} />
              ))}
            </div>
          )}
        </div>

        {/* Activity feed */}
        <div>
          <h2 className="mb-4 text-lg font-semibold text-claw-accent">Recent Activity</h2>
          {activity.length === 0 ? (
            <p className="text-claw-dim">No recent activity.</p>
          ) : (
            <div className="space-y-2">
              {activity.slice(0, 20).map((event, i) => (
                <div
                  key={`${event.ts}-${i}`}
                  className="rounded-lg border border-claw-border bg-claw-card p-3"
                >
                  <div className="flex items-center justify-between text-xs text-claw-dim">
                    <span>{event.agent}</span>
                    <span>{event.ts ? new Date(event.ts).toLocaleTimeString() : ''}</span>
                  </div>
                  <p className="mt-1 text-sm">{event.message}</p>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function SummaryCard({
  label,
  value,
  color,
}: {
  label: string
  value: number
  color: string
}) {
  return (
    <div className="rounded-lg border border-claw-border bg-claw-card p-4 text-center">
      <div className={`text-3xl font-bold ${color}`}>{value}</div>
      <div className="mt-1 text-xs uppercase tracking-wide text-claw-dim">{label}</div>
    </div>
  )
}

function FleetGraph({ agents }: { agents: ClawAgent[] }) {
  const { nodes, edges, harnessTypes, hasEphemeral, hasPersistent } = useMemo(() => {
    const harnessSet = new Set<string>()
    let hasEph = false
    let hasPer = false

    // Build nodes for ALL agents
    const nodes = agents.map((a, i) => {
      const harness = a.spec.harness?.type ?? 'openclaw'
      const ws = a.spec.workspace?.mode ?? 'ephemeral'
      harnessSet.add(harness)
      if (ws === 'persistent') hasPer = true; else hasEph = true
      return {
        name: a.metadata.name,
        harness,
        workspace: ws,
        phase: a.status?.phase ?? 'Unknown',
        a2a: !!a.spec.a2a?.enabled,
        index: i,
      }
    })

    // Build edges from A2A peers
    const edges: { from: string; to: string }[] = []
    for (const a of agents) {
      if (!a.spec.a2a?.enabled) continue
      for (const peer of a.spec.a2a.peers ?? []) {
        // Deduplicate bidirectional
        if (!edges.some(e => (e.from === peer.name && e.to === a.metadata.name))) {
          edges.push({ from: a.metadata.name, to: peer.name })
        }
      }
    }

    return { nodes, edges, harnessTypes: Array.from(harnessSet), hasEphemeral: hasEph, hasPersistent: hasPer }
  }, [agents])

  // Layout: circular for A2A nodes, row below for non-A2A
  const W = 600
  const H = 340
  const cx = W / 2
  const cy = 140

  const a2aNodes = nodes.filter(n => n.a2a)
  const otherNodes = nodes.filter(n => !n.a2a)
  const r = Math.max(80, Math.min(120, a2aNodes.length * 30))

  const positions = new Map<string, { x: number; y: number }>()

  // A2A nodes in a circle
  a2aNodes.forEach((n, i) => {
    const angle = (2 * Math.PI * i) / Math.max(a2aNodes.length, 1) - Math.PI / 2
    positions.set(n.name, {
      x: cx + r * Math.cos(angle),
      y: cy + r * Math.sin(angle),
    })
  })

  // Non-A2A nodes in a row below
  const rowY = cy + r + 70
  const rowSpacing = Math.min(120, (W - 80) / Math.max(otherNodes.length, 1))
  const rowStart = cx - ((otherNodes.length - 1) * rowSpacing) / 2
  otherNodes.forEach((n, i) => {
    positions.set(n.name, { x: rowStart + i * rowSpacing, y: rowY })
  })

  const nodeRadius = 28

  return (
    <div className="rounded-lg border border-claw-border bg-claw-card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-claw-accent">Fleet Topology</h2>
        {/* Legend */}
        <div className="flex flex-wrap gap-3 text-xs">
          {harnessTypes.map(h => (
            <span key={h} className="flex items-center gap-1.5">
              <span className="inline-block h-3 w-3 rounded-full" style={{ background: harnessColor(h) }} />
              {h}
            </span>
          ))}
          {hasPersistent && (
            <span className="flex items-center gap-1.5">
              <span className="inline-block h-3 w-3 rounded-full border-2 border-claw-text" /> persistent
            </span>
          )}
          {hasEphemeral && (
            <span className="flex items-center gap-1.5">
              <span className="inline-block h-3 w-3 rounded-full border-2 border-dashed border-claw-dim" /> ephemeral
            </span>
          )}
        </div>
      </div>

      <svg width="100%" viewBox={`0 0 ${W} ${H + (otherNodes.length > 0 ? 40 : 0)}`} className="mx-auto max-w-2xl">
        {/* A2A connection edges */}
        {edges.map((e, i) => {
          const from = positions.get(e.from)
          const to = positions.get(e.to)
          if (!from || !to) return null
          return (
            <line
              key={i}
              x1={from.x} y1={from.y}
              x2={to.x} y2={to.y}
              stroke="#4ecca3"
              strokeWidth={2}
              strokeDasharray="6 3"
              opacity={0.5}
            />
          )
        })}

        {/* Agent nodes */}
        {nodes.map(n => {
          const pos = positions.get(n.name)
          if (!pos) return null
          const color = harnessColor(n.harness)
          const isEphemeral = n.workspace === 'ephemeral'
          const phaseOpacity = n.phase === 'Running' ? 1 : 0.5

          return (
            <g key={n.name} opacity={phaseOpacity}>
              {/* Node circle */}
              <circle
                cx={pos.x} cy={pos.y} r={nodeRadius}
                fill="#1a1a2e"
                stroke={color}
                strokeWidth={2.5}
                strokeDasharray={isEphemeral ? '5 3' : 'none'}
              />
              {/* A2A indicator ring */}
              {n.a2a && (
                <circle
                  cx={pos.x} cy={pos.y} r={nodeRadius + 5}
                  fill="none"
                  stroke={color}
                  strokeWidth={1}
                  opacity={0.3}
                />
              )}
              {/* Agent name */}
              <text
                x={pos.x} y={pos.y + 1}
                textAnchor="middle"
                dominantBaseline="middle"
                fill="#e0e0e0"
                fontSize={9}
                fontWeight={500}
              >
                {n.name.length > 12 ? n.name.slice(0, 11) + '…' : n.name}
              </text>
              {/* Harness label below */}
              <text
                x={pos.x} y={pos.y + nodeRadius + 14}
                textAnchor="middle"
                fill={color}
                fontSize={8}
                opacity={0.7}
              >
                {n.harness}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

function AgentCard({ agent }: { agent: ClawAgent }) {
  const phase = agent.status?.phase ?? 'Unknown'
  const model = agent.spec.model?.name ?? 'not set'
  const provider = agent.spec.model?.provider ?? ''
  const harness = agent.spec.harness?.type ?? 'openclaw'
  const image = agent.spec.harness?.image ?? ''
  const channels = agent.spec.channels ?? []
  const workspace = agent.spec.workspace?.mode ?? 'ephemeral'
  const soul = agent.spec.identity?.soul ?? ''
  const soulSnippet = soul.length > 80 ? soul.slice(0, 80) + '...' : soul

  return (
    <div className="rounded-lg border border-claw-border bg-claw-card">
      <div className="flex items-center justify-between bg-claw-border/50 px-4 py-2.5">
        <span className="font-semibold">{agent.metadata.name}</span>
        <StatusBadge phase={phase} />
      </div>
      <div className="space-y-1.5 p-4 text-sm">
        <div className="flex flex-wrap gap-1.5">
          <span className="rounded bg-claw-border/60 px-1.5 py-0.5 text-xs">{harness}</span>
          {channels.map(ch => (
            <span key={ch} className="rounded bg-claw-accent/20 text-claw-accent px-1.5 py-0.5 text-xs">{ch}</span>
          ))}
          <span className="rounded bg-claw-border/40 px-1.5 py-0.5 text-xs text-claw-dim">{workspace}</span>
        </div>
        <div>
          <span className="text-claw-dim">Model: </span>
          {provider ? `${provider} / ${model}` : model}
        </div>
        {image && (
          <div>
            <span className="text-claw-dim">Image: </span>
            <span className="text-xs font-mono">{image}</span>
          </div>
        )}
        {soulSnippet && (
          <div className="mt-2 rounded border-l-2 border-claw-border bg-claw-border/20 px-3 py-2 text-xs italic text-claw-dim">
            "{soulSnippet}"
          </div>
        )}
      </div>
    </div>
  )
}
