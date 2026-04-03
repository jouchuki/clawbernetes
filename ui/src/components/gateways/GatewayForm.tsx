import { useState } from 'react'
import type { ClawGateway, ClawGatewaySpec, GatewayEvaluatorSpec } from '../../api/types'
import FormField, { inputClass, selectClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'
import ToggleSwitch from '../shared/ToggleSwitch'
import RemoveButton from '../shared/RemoveButton'

interface GatewayFormProps {
  initialData?: ClawGateway | null
  onSubmit: (name: string, spec: ClawGatewaySpec) => Promise<void>
}

export default function GatewayForm({ initialData, onSubmit }: GatewayFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [spec, setSpec] = useState<ClawGatewaySpec>(initialData?.spec ?? {})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const evaluators = spec.routing?.evaluators ?? []

  const setEvaluator = (idx: number, patch: Partial<GatewayEvaluatorSpec>) => {
    const next = [...evaluators]
    next[idx] = { ...next[idx], ...patch }
    setSpec((s) => ({ ...s, routing: { ...s.routing, evaluators: next } }))
  }

  const addEvaluator = () => {
    setSpec((s) => ({ ...s, routing: { ...s.routing, evaluators: [...evaluators, {}] } }))
  }

  const removeEvaluator = (idx: number) => {
    setSpec((s) => ({ ...s, routing: { ...s.routing, evaluators: evaluators.filter((_, i) => i !== idx) } }))
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
    <form onSubmit={handleSubmit} className="mx-auto max-w-3xl space-y-6">
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Gateway' : 'Create Gateway'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Port" htmlFor="port">
            <input id="port" className={inputClass} type="number" value={spec.port ?? ''} onChange={(e) => setSpec((s) => ({ ...s, port: e.target.value ? Number(e.target.value) : undefined }))} />
          </FormField>
          <FormField label="Mode" htmlFor="topology">
            <select id="topology" className={selectClass} value={spec.topology ?? 'sidecar'} onChange={(e) => setSpec((s) => ({ ...s, topology: e.target.value }))}>
              <option value="sidecar">Sidecar</option>
              <option value="centralized">Centralized</option>
            </select>
          </FormField>
        </div>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Routing</h2>
        <ToggleSwitch label="Log Every Decision" enabled={!!spec.routing?.logEveryDecision} onChange={(v) => setSpec((s) => ({ ...s, routing: { ...s.routing, logEveryDecision: v } }))} />

        <div className="mb-2 flex items-center justify-between">
          <label className="text-sm font-medium text-claw-dim">Evaluators</label>
          <button type="button" onClick={addEvaluator} className="rounded bg-claw-accent/20 px-3 py-1 text-xs text-claw-accent hover:bg-claw-accent/30">
            + Add Evaluator
          </button>
        </div>
        {evaluators.map((ev, idx) => (
          <div key={idx} className="mb-3 rounded border border-claw-border bg-claw-bg p-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs text-claw-dim">Evaluator {idx + 1}</span>
              <RemoveButton onClick={() => removeEvaluator(idx)} />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <FormField label="Name"><input className={inputClass} value={ev.name ?? ''} onChange={(e) => setEvaluator(idx, { name: e.target.value })} /></FormField>
              <FormField label="Type"><input className={inputClass} value={ev.type ?? ''} onChange={(e) => setEvaluator(idx, { type: e.target.value })} /></FormField>
              <FormField label="Priority"><input className={inputClass} type="number" value={ev.priority ?? ''} onChange={(e) => setEvaluator(idx, { priority: e.target.value ? Number(e.target.value) : undefined })} /></FormField>
              <FormField label="Action"><input className={inputClass} value={ev.action ?? ''} onChange={(e) => setEvaluator(idx, { action: e.target.value })} /></FormField>
            </div>
            <FormField label="Patterns (comma-separated)">
              <input className={inputClass} value={(ev.patterns ?? []).join(', ')} onChange={(e) => setEvaluator(idx, { patterns: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })} />
            </FormField>
          </div>
        ))}
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Anomaly Detection</h2>
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Spend Spike Multiplier" htmlFor="spendSpike">
            <input id="spendSpike" className={inputClass} type="number" step="0.1" value={spec.anomaly?.spendSpikeMultiplier ?? ''} onChange={(e) => setSpec((s) => ({ ...s, anomaly: { ...s.anomaly, spendSpikeMultiplier: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Idle Burn Minutes" htmlFor="idleBurn">
            <input id="idleBurn" className={inputClass} type="number" value={spec.anomaly?.idleBurnMinutes ?? ''} onChange={(e) => setSpec((s) => ({ ...s, anomaly: { ...s.anomaly, idleBurnMinutes: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Error Loop Threshold" htmlFor="errorLoop">
            <input id="errorLoop" className={inputClass} type="number" value={spec.anomaly?.errorLoopThreshold ?? ''} onChange={(e) => setSpec((s) => ({ ...s, anomaly: { ...s.anomaly, errorLoopThreshold: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Token Inflation Multiplier" htmlFor="tokenInflation">
            <input id="tokenInflation" className={inputClass} type="number" step="0.1" value={spec.anomaly?.tokenInflationMultiplier ?? ''} onChange={(e) => setSpec((s) => ({ ...s, anomaly: { ...s.anomaly, tokenInflationMultiplier: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Check Interval (seconds)" htmlFor="checkInterval">
            <input id="checkInterval" className={inputClass} type="number" value={spec.anomaly?.checkIntervalSeconds ?? ''} onChange={(e) => setSpec((s) => ({ ...s, anomaly: { ...s.anomaly, checkIntervalSeconds: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
        </div>
      </div>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}
