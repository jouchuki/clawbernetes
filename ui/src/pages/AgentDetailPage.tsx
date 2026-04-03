import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiFetch } from '../api/client'
import type { ClawAgent } from '../api/types'
import StatusBadge from '../components/shared/StatusBadge'
import ErrorAlert from '../components/shared/ErrorAlert'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import LogStream from '../components/logs/LogStream'
import ChatPanel from '../components/chat/ChatPanel'
import YamlViewer from '../components/shared/YamlViewer'

type Tab = 'overview' | 'logs' | 'chat' | 'config'

const TABS: { key: Tab; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'logs', label: 'Logs' },
  { key: 'chat', label: 'Chat' },
  { key: 'config', label: 'Config' },
]

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-claw-border bg-claw-card p-4">
      <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-claw-dim">
        {title}
      </h3>
      {children}
    </div>
  )
}

function Field({ label, value }: { label: string; value?: string | null }) {
  if (!value) return null
  return (
    <div className="mb-2">
      <span className="text-xs text-claw-dim">{label}</span>
      <p className="text-sm text-claw-text">{value}</p>
    </div>
  )
}

function Badge({ text }: { text: string }) {
  return (
    <span className="inline-block rounded-full bg-claw-border/40 px-2.5 py-0.5 text-xs text-claw-text">
      {text}
    </span>
  )
}

function OverviewTab({ agent }: { agent: ClawAgent }) {
  const { spec, status } = agent
  const [soulExpanded, setSoulExpanded] = useState(false)
  const soul = spec.identity?.soul

  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Section title="Status">
        <Field label="Phase" value={status?.phase} />
        <Field label="Pod Name" value={status?.podName} />
        <Field label="Workspace PVC" value={status?.workspacePVC} />
        {status?.conditions && status.conditions.length > 0 && (
          <div className="mt-2">
            <span className="text-xs text-claw-dim">Conditions</span>
            <div className="mt-1 space-y-1">
              {status.conditions.map((c, i) => (
                <div key={i} className="text-xs text-claw-text">
                  <span className="font-medium">{c.type}</span>: {c.status}
                  {c.reason && <span className="text-claw-dim"> ({c.reason})</span>}
                </div>
              ))}
            </div>
          </div>
        )}
      </Section>

      <Section title="Harness">
        <Field label="Type" value={spec.harness?.type ?? 'openclaw'} />
        <Field label="Image" value={spec.harness?.image || 'default'} />
      </Section>

      <Section title="Model">
        <Field
          label="Provider / Name"
          value={
            spec.model?.provider || spec.model?.name
              ? `${spec.model.provider ?? ''}/${spec.model.name ?? ''}`
              : undefined
          }
        />
        <Field
          label="Fallback"
          value={
            spec.model?.fallback?.provider || spec.model?.fallback?.name
              ? `${spec.model.fallback.provider ?? ''}/${spec.model.fallback.name ?? ''}`
              : undefined
          }
        />
      </Section>

      {soul && (
        <Section title="Identity">
          <div className="rounded border border-claw-border bg-claw-bg p-3 text-sm text-claw-text">
            {soul.length > 200 && !soulExpanded ? (
              <>
                {soul.slice(0, 200)}...
                <button
                  onClick={() => setSoulExpanded(true)}
                  className="ml-1 text-claw-accent text-xs"
                >
                  Expand
                </button>
              </>
            ) : (
              <>
                {soul}
                {soul.length > 200 && (
                  <button
                    onClick={() => setSoulExpanded(false)}
                    className="ml-1 text-claw-accent text-xs"
                  >
                    Collapse
                  </button>
                )}
              </>
            )}
          </div>
        </Section>
      )}

      <Section title="References">
        <Field label="Policy" value={spec.policy} />
        <Field label="Gateway" value={spec.gateway} />
        <Field label="Observability" value={spec.observability} />
        <Field label="Skill Set" value={spec.skillSet} />
        <Field label="Credentials Secret" value={spec.credentialsSecret} />
      </Section>

      {spec.channels && spec.channels.length > 0 && (
        <Section title="Channels">
          <div className="flex flex-wrap gap-2">
            {spec.channels.map((ch) => (
              <Badge key={ch} text={ch} />
            ))}
          </div>
        </Section>
      )}

      {spec.workspace && (
        <Section title="Workspace">
          <Field label="Mode" value={spec.workspace.mode} />
          <Field label="Storage Size" value={spec.workspace.storageSize} />
          <Field label="Storage Class" value={spec.workspace.storageClassName} />
          <Field label="Reclaim Policy" value={spec.workspace.reclaimPolicy} />
        </Section>
      )}

      {spec.a2a && (
        <Section title="A2A">
          <Field label="Enabled" value={spec.a2a.enabled ? 'Yes' : 'No'} />
          <Field label="Card Name" value={spec.a2a.agentCardName} />
          <Field label="Card Description" value={spec.a2a.agentCardDescription} />
          {spec.a2a.skills && spec.a2a.skills.length > 0 && (
            <div className="mb-2">
              <span className="text-xs text-claw-dim">Skills</span>
              <div className="mt-1 flex flex-wrap gap-1">
                {spec.a2a.skills.map((s) => (
                  <Badge key={s} text={s} />
                ))}
              </div>
            </div>
          )}
          {spec.a2a.peers && spec.a2a.peers.length > 0 && (
            <div>
              <span className="text-xs text-claw-dim">Peers</span>
              <div className="mt-1 flex flex-wrap gap-1">
                {spec.a2a.peers.map((p) => (
                  <Badge key={p.name} text={p.name} />
                ))}
              </div>
            </div>
          )}
        </Section>
      )}

      {spec.resources && (
        <Section title="Resources">
          {spec.resources.requests && (
            <div className="mb-2">
              <span className="text-xs text-claw-dim">Requests</span>
              {Object.entries(spec.resources.requests).map(([k, v]) => (
                <p key={k} className="text-sm text-claw-text">
                  {k}: {v}
                </p>
              ))}
            </div>
          )}
          {spec.resources.limits && (
            <div>
              <span className="text-xs text-claw-dim">Limits</span>
              {Object.entries(spec.resources.limits).map(([k, v]) => (
                <p key={k} className="text-sm text-claw-text">
                  {k}: {v}
                </p>
              ))}
            </div>
          )}
        </Section>
      )}

      {spec.lifecycle && (
        <Section title="Lifecycle">
          <Field label="Restart Policy" value={spec.lifecycle.restartPolicy} />
          <Field
            label="Hibernate After Idle"
            value={
              spec.lifecycle.hibernateAfterIdleMinutes != null
                ? `${spec.lifecycle.hibernateAfterIdleMinutes} min`
                : undefined
            }
          />
          <Field label="Max Runtime" value={spec.lifecycle.maxRuntime} />
        </Section>
      )}
    </div>
  )
}

export default function AgentDetailPage() {
  const { name } = useParams<{ name: string }>()
  const [agent, setAgent] = useState<ClawAgent | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [tab, setTab] = useState<Tab>('overview')

  useEffect(() => {
    if (!name) return
    setLoading(true)
    apiFetch<ClawAgent>(`/api/agents/${name}`)
      .then((a) => {
        setAgent(a)
        setError(null)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [name])

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />
  if (!agent) return <ErrorAlert message="Agent not found" />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link
            to="/agents"
            className="text-claw-dim hover:text-claw-text text-sm"
          >
            Agents /
          </Link>
          <h1 className="text-2xl font-bold text-claw-text">
            {agent.metadata.name}
          </h1>
          <StatusBadge phase={agent.status?.phase ?? 'Unknown'} />
        </div>
        <Link
          to={`/agents/${name}/edit`}
          className="rounded-lg border border-claw-border bg-claw-card px-4 py-2 text-sm text-claw-text hover:border-claw-accent"
        >
          Edit
        </Link>
      </div>

      <div className="mb-6 flex gap-2">
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`rounded-full px-4 py-1.5 text-sm font-medium transition-colors ${
              tab === t.key
                ? 'bg-claw-accent text-claw-bg'
                : 'bg-claw-card text-claw-dim hover:text-claw-text'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'overview' && <OverviewTab agent={agent} />}
      {tab === 'logs' && <LogStream agentName={name!} enabled />}
      {tab === 'chat' && <ChatPanel agentName={name!} />}
      {tab === 'config' && <YamlViewer data={agent} />}
    </div>
  )
}
