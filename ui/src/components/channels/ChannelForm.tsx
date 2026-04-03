import { useState } from 'react'
import type { ClawChannel, ClawChannelSpec } from '../../api/types'
import FormField, { inputClass, selectClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'
import RemoveButton from '../shared/RemoveButton'

interface ChannelFormProps {
  initialData?: ClawChannel | null
  onSubmit: (name: string, spec: ClawChannelSpec) => Promise<void>
}

export default function ChannelForm({ initialData, onSubmit }: ChannelFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [spec, setSpec] = useState<ClawChannelSpec>(initialData?.spec ?? { type: 'telegram', credentialsSecret: '' })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const configEntries = Object.entries(spec.config ?? {})

  const setConfig = (entries: [string, string][]) => {
    const config: Record<string, string> = {}
    for (const [k, v] of entries) if (k) config[k] = v
    setSpec((s) => ({ ...s, config }))
  }

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

  return (
    <form onSubmit={handleSubmit} className="mx-auto max-w-2xl space-y-6">
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Channel' : 'Create Channel'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
        <FormField label="Type" htmlFor="type">
          <select id="type" className={selectClass} value={spec.type} onChange={(e) => setSpec((s) => ({ ...s, type: e.target.value }))}>
            <option value="telegram">Telegram</option>
            <option value="slack">Slack</option>
            <option value="discord">Discord</option>
            <option value="whatsapp">WhatsApp</option>
            <option value="signal">Signal</option>
          </select>
        </FormField>
        <FormField label="Credentials Secret" htmlFor="credSecret">
          <input id="credSecret" className={inputClass} value={spec.credentialsSecret} onChange={(e) => setSpec((s) => ({ ...s, credentialsSecret: e.target.value }))} required />
        </FormField>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <div className="mb-2 flex items-center justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-claw-dim">Config</h2>
          <button type="button" onClick={() => setConfig([...configEntries, ['', '']])} className="rounded bg-claw-accent/20 px-3 py-1 text-xs text-claw-accent hover:bg-claw-accent/30">
            + Add Entry
          </button>
        </div>
        {configEntries.map(([k, v], idx) => (
          <div key={idx} className="mb-2 flex gap-2">
            <input className={inputClass} placeholder="Key" value={k} onChange={(e) => { const next = [...configEntries] as [string, string][]; next[idx] = [e.target.value, v]; setConfig(next) }} />
            <input className={inputClass} placeholder="Value" value={v} onChange={(e) => { const next = [...configEntries] as [string, string][]; next[idx] = [k, e.target.value]; setConfig(next) }} />
            <RemoveButton onClick={() => setConfig(configEntries.filter((_, i) => i !== idx) as [string, string][])} />
          </div>
        ))}
      </div>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}
