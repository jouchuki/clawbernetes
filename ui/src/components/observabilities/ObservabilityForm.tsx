import { useState } from 'react'
import type { ClawObservability, ClawObservabilitySpec } from '../../api/types'
import FormField, { inputClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'
import ToggleSwitch from '../shared/ToggleSwitch'

interface ObservabilityFormProps {
  initialData?: ClawObservability | null
  onSubmit: (name: string, spec: ClawObservabilitySpec) => Promise<void>
}

export default function ObservabilityForm({ initialData, onSubmit }: ObservabilityFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [spec, setSpec] = useState<ClawObservabilitySpec>(initialData?.spec ?? {})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

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
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Observability' : 'Create Observability'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
        <FormField label="OTLP Endpoint" htmlFor="otlpEndpoint">
          <input id="otlpEndpoint" className={inputClass} value={spec.otlpEndpoint ?? ''} onChange={(e) => setSpec((s) => ({ ...s, otlpEndpoint: e.target.value }))} />
        </FormField>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Tempo</h2>
        <ToggleSwitch label="Enabled" enabled={!!spec.tempo?.enabled} onChange={(v) => setSpec((s) => ({ ...s, tempo: { ...s.tempo, enabled: v } }))} />
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Grafana</h2>
        <ToggleSwitch label="Enabled" enabled={!!spec.grafana?.enabled} onChange={(v) => setSpec((s) => ({ ...s, grafana: { ...s.grafana, enabled: v } }))} />
      </div>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}
