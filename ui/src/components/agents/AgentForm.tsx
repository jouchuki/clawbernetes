import { useState } from 'react'
import type { ClawAgent, ClawAgentSpec, A2APeer } from '../../api/types'
import FormField, { inputClass, selectClass, textareaClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'
import ToggleSwitch from '../shared/ToggleSwitch'
import RemoveButton from '../shared/RemoveButton'

interface AgentFormProps {
  initialData?: ClawAgent | null
  onSubmit: (name: string, spec: ClawAgentSpec) => Promise<void>
}

export default function AgentForm({ initialData, onSubmit }: AgentFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [spec, setSpec] = useState<ClawAgentSpec>(initialData?.spec ?? {})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const update = (patch: Partial<ClawAgentSpec>) => setSpec((s) => ({ ...s, ...patch }))

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await onSubmit(name, spec)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Submit failed')
    } finally {
      setSubmitting(false)
    }
  }

  const peers = spec.a2a?.peers ?? []

  const setPeer = (idx: number, patch: Partial<A2APeer>) => {
    const next = [...peers]
    next[idx] = { ...next[idx], ...patch }
    update({ a2a: { ...spec.a2a, peers: next } })
  }

  const addPeer = () => {
    update({ a2a: { ...spec.a2a, peers: [...peers, { name: '', agentCardUrl: '' }] } })
  }

  const removePeer = (idx: number) => {
    update({ a2a: { ...spec.a2a, peers: peers.filter((_, i) => i !== idx) } })
  }

  return (
    <form onSubmit={handleSubmit} className="mx-auto max-w-3xl space-y-6">
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Agent' : 'Create Agent'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <Section title="Metadata">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
      </Section>

      <Section title="Identity">
        <FormField label="Soul" htmlFor="soul">
          <textarea id="soul" className={textareaClass} rows={4} value={spec.identity?.soul ?? ''} onChange={(e) => update({ identity: { ...spec.identity, soul: e.target.value } })} />
        </FormField>
        <FormField label="User" htmlFor="user">
          <textarea id="user" className={textareaClass} rows={2} value={spec.identity?.user ?? ''} onChange={(e) => update({ identity: { ...spec.identity, user: e.target.value } })} />
        </FormField>
        <FormField label="Agent Identity" htmlFor="agentIdentity">
          <textarea id="agentIdentity" className={textareaClass} rows={2} value={spec.identity?.agentIdentity ?? ''} onChange={(e) => update({ identity: { ...spec.identity, agentIdentity: e.target.value } })} />
        </FormField>
      </Section>

      <Section title="Model">
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Provider" htmlFor="modelProvider">
            <input id="modelProvider" className={inputClass} value={spec.model?.provider ?? ''} onChange={(e) => update({ model: { ...spec.model, provider: e.target.value } })} />
          </FormField>
          <FormField label="Name" htmlFor="modelName">
            <input id="modelName" className={inputClass} value={spec.model?.name ?? ''} onChange={(e) => update({ model: { ...spec.model, name: e.target.value } })} />
          </FormField>
          <FormField label="Fallback Provider" htmlFor="fbProvider">
            <input id="fbProvider" className={inputClass} value={spec.model?.fallback?.provider ?? ''} onChange={(e) => update({ model: { ...spec.model, fallback: { ...spec.model?.fallback, provider: e.target.value } } })} />
          </FormField>
          <FormField label="Fallback Name" htmlFor="fbName">
            <input id="fbName" className={inputClass} value={spec.model?.fallback?.name ?? ''} onChange={(e) => update({ model: { ...spec.model, fallback: { ...spec.model?.fallback, name: e.target.value } } })} />
          </FormField>
        </div>
      </Section>

      <Section title="References">
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Policy" htmlFor="policy">
            <input id="policy" className={inputClass} value={spec.policy ?? ''} onChange={(e) => update({ policy: e.target.value })} />
          </FormField>
          <FormField label="Gateway" htmlFor="gateway">
            <input id="gateway" className={inputClass} value={spec.gateway ?? ''} onChange={(e) => update({ gateway: e.target.value })} />
          </FormField>
          <FormField label="Observability" htmlFor="observability">
            <input id="observability" className={inputClass} value={spec.observability ?? ''} onChange={(e) => update({ observability: e.target.value })} />
          </FormField>
          <FormField label="Skill Set" htmlFor="skillSet">
            <input id="skillSet" className={inputClass} value={spec.skillSet ?? ''} onChange={(e) => update({ skillSet: e.target.value })} />
          </FormField>
        </div>
      </Section>

      <Section title="Channels">
        <FormField label="Channels (comma-separated)" htmlFor="channels">
          <input
            id="channels"
            className={inputClass}
            value={(spec.channels ?? []).join(', ')}
            onChange={(e) => update({ channels: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })}
          />
        </FormField>
      </Section>

      <Section title="Workspace">
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Mode" htmlFor="wsMode">
            <select id="wsMode" className={selectClass} value={spec.workspace?.mode ?? 'ephemeral'} onChange={(e) => update({ workspace: { ...spec.workspace, mode: e.target.value } })}>
              <option value="ephemeral">Ephemeral</option>
              <option value="persistent">Persistent</option>
            </select>
          </FormField>
          {spec.workspace?.mode === 'persistent' && (
            <FormField label="Storage Size" htmlFor="storageSize">
              <input id="storageSize" className={inputClass} value={spec.workspace?.storageSize ?? ''} onChange={(e) => update({ workspace: { ...spec.workspace, storageSize: e.target.value } })} placeholder="e.g. 1Gi" />
            </FormField>
          )}
        </div>
      </Section>

      <Section title="Resources">
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Requests Memory" htmlFor="reqMem">
            <input id="reqMem" className={inputClass} value={spec.resources?.requests?.memory ?? ''} onChange={(e) => update({ resources: { ...spec.resources, requests: { ...spec.resources?.requests, memory: e.target.value } } })} placeholder="e.g. 256Mi" />
          </FormField>
          <FormField label="Requests CPU" htmlFor="reqCpu">
            <input id="reqCpu" className={inputClass} value={spec.resources?.requests?.cpu ?? ''} onChange={(e) => update({ resources: { ...spec.resources, requests: { ...spec.resources?.requests, cpu: e.target.value } } })} placeholder="e.g. 250m" />
          </FormField>
          <FormField label="Limits Memory" htmlFor="limMem">
            <input id="limMem" className={inputClass} value={spec.resources?.limits?.memory ?? ''} onChange={(e) => update({ resources: { ...spec.resources, limits: { ...spec.resources?.limits, memory: e.target.value } } })} placeholder="e.g. 512Mi" />
          </FormField>
          <FormField label="Limits CPU" htmlFor="limCpu">
            <input id="limCpu" className={inputClass} value={spec.resources?.limits?.cpu ?? ''} onChange={(e) => update({ resources: { ...spec.resources, limits: { ...spec.resources?.limits, cpu: e.target.value } } })} placeholder="e.g. 500m" />
          </FormField>
        </div>
      </Section>

      <Section title="Lifecycle">
        <div className="grid grid-cols-3 gap-4">
          <FormField label="Restart Policy" htmlFor="restartPolicy">
            <select id="restartPolicy" className={selectClass} value={spec.lifecycle?.restartPolicy ?? 'Always'} onChange={(e) => update({ lifecycle: { ...spec.lifecycle, restartPolicy: e.target.value } })}>
              <option value="Always">Always</option>
              <option value="Never">Never</option>
              <option value="OnFailure">OnFailure</option>
            </select>
          </FormField>
          <FormField label="Max Runtime" htmlFor="maxRuntime">
            <input id="maxRuntime" className={inputClass} value={spec.lifecycle?.maxRuntime ?? ''} onChange={(e) => update({ lifecycle: { ...spec.lifecycle, maxRuntime: e.target.value } })} placeholder="e.g. 24h" />
          </FormField>
          <FormField label="Hibernate After Idle (min)" htmlFor="hibernate">
            <input id="hibernate" className={inputClass} type="number" value={spec.lifecycle?.hibernateAfterIdleMinutes ?? ''} onChange={(e) => update({ lifecycle: { ...spec.lifecycle, hibernateAfterIdleMinutes: e.target.value ? Number(e.target.value) : undefined } })} />
          </FormField>
        </div>
      </Section>

      <Section title="Credentials">
        <FormField label="Credentials Secret" htmlFor="credSecret">
          <input id="credSecret" className={inputClass} value={spec.credentialsSecret ?? ''} onChange={(e) => update({ credentialsSecret: e.target.value })} />
        </FormField>
      </Section>

      <Section title="A2A (Agent-to-Agent)">
        <ToggleSwitch label="Enabled" enabled={!!spec.a2a?.enabled} onChange={(v) => update({ a2a: { ...spec.a2a, enabled: v } })} />
        {spec.a2a?.enabled && (
          <>
            <div className="grid grid-cols-2 gap-4">
              <FormField label="Agent Card Name" htmlFor="a2aCardName">
                <input id="a2aCardName" className={inputClass} value={spec.a2a?.agentCardName ?? ''} onChange={(e) => update({ a2a: { ...spec.a2a, agentCardName: e.target.value } })} />
              </FormField>
              <FormField label="Skills (comma-separated)" htmlFor="a2aSkills">
                <input id="a2aSkills" className={inputClass} value={(spec.a2a?.skills ?? []).join(', ')} onChange={(e) => update({ a2a: { ...spec.a2a, skills: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) } })} />
              </FormField>
            </div>
            <div className="mt-4">
              <div className="mb-2 flex items-center justify-between">
                <label className="text-sm font-medium text-claw-dim">Peers</label>
                <button type="button" onClick={addPeer} className="rounded bg-claw-accent/20 px-3 py-1 text-xs text-claw-accent hover:bg-claw-accent/30">
                  + Add Peer
                </button>
              </div>
              {peers.map((peer, idx) => (
                <div key={idx} className="mb-2 flex gap-2">
                  <input className={inputClass} placeholder="Name" value={peer.name} onChange={(e) => setPeer(idx, { name: e.target.value })} />
                  <input className={inputClass} placeholder="Agent Card URL" value={peer.agentCardUrl} onChange={(e) => setPeer(idx, { agentCardUrl: e.target.value })} />
                  <input className={inputClass} placeholder="Credentials Secret" value={peer.credentialsSecret ?? ''} onChange={(e) => setPeer(idx, { credentialsSecret: e.target.value })} />
                  <RemoveButton onClick={() => removePeer(idx)} />
                </div>
              ))}
            </div>
          </>
        )}
      </Section>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-claw-border bg-claw-card p-4">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">{title}</h2>
      {children}
    </div>
  )
}
