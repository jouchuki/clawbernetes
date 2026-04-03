import { useState } from 'react'
import type { ClawPolicy, ClawPolicySpec } from '../../api/types'
import FormField, { inputClass, textareaClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'

interface PolicyFormProps {
  initialData?: ClawPolicy | null
  onSubmit: (name: string, spec: ClawPolicySpec) => Promise<void>
}

export default function PolicyForm({ initialData, onSubmit }: PolicyFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [spec, setSpec] = useState<ClawPolicySpec>(initialData?.spec ?? {})
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
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Policy' : 'Create Policy'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Budget</h2>
        <div className="grid grid-cols-2 gap-4">
          <FormField label="Daily" htmlFor="daily">
            <input id="daily" className={inputClass} type="number" value={spec.budget?.daily ?? ''} onChange={(e) => setSpec((s) => ({ ...s, budget: { ...s.budget, daily: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Monthly" htmlFor="monthly">
            <input id="monthly" className={inputClass} type="number" value={spec.budget?.monthly ?? ''} onChange={(e) => setSpec((s) => ({ ...s, budget: { ...s.budget, monthly: e.target.value ? Number(e.target.value) : undefined } }))} />
          </FormField>
          <FormField label="Warn At" htmlFor="warnAt">
            <input id="warnAt" className={inputClass} value={spec.budget?.warnAt ?? ''} onChange={(e) => setSpec((s) => ({ ...s, budget: { ...s.budget, warnAt: e.target.value } }))} placeholder="e.g. 80%" />
          </FormField>
          <FormField label="Downgrade Model" htmlFor="downModel">
            <input id="downModel" className={inputClass} value={spec.budget?.downgradeModel ?? ''} onChange={(e) => setSpec((s) => ({ ...s, budget: { ...s.budget, downgradeModel: e.target.value } }))} />
          </FormField>
          <FormField label="Downgrade Provider" htmlFor="downProvider">
            <input id="downProvider" className={inputClass} value={spec.budget?.downgradeProvider ?? ''} onChange={(e) => setSpec((s) => ({ ...s, budget: { ...s.budget, downgradeProvider: e.target.value } }))} />
          </FormField>
        </div>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-claw-dim">Tool Policy</h2>
        <FormField label="Allow (one per line)" htmlFor="allow">
          <textarea id="allow" className={textareaClass} rows={4} value={(spec.toolPolicy?.allow ?? []).join('\n')} onChange={(e) => setSpec((s) => ({ ...s, toolPolicy: { ...s.toolPolicy, allow: e.target.value.split('\n').filter(Boolean) } }))} />
        </FormField>
        <FormField label="Deny (one per line)" htmlFor="deny">
          <textarea id="deny" className={textareaClass} rows={4} value={(spec.toolPolicy?.deny ?? []).join('\n')} onChange={(e) => setSpec((s) => ({ ...s, toolPolicy: { ...s.toolPolicy, deny: e.target.value.split('\n').filter(Boolean) } }))} />
        </FormField>
      </div>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}
