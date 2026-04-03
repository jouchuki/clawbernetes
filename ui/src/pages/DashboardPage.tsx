import { useEffect, useState } from 'react'
import { apiFetch } from '../api/client'
import type { FleetSummary, ClawAgent, ActivityEvent } from '../api/types'
import StatusBadge from '../components/shared/StatusBadge'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

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
