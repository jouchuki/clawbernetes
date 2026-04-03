import { useState } from 'react'
import type { ClawSkillSet, ClawSkillSetSpec, SkillEntry } from '../../api/types'
import FormField, { inputClass, textareaClass } from '../shared/FormField'
import FormActions from '../shared/FormActions'
import RemoveButton from '../shared/RemoveButton'

interface SkillSetFormProps {
  initialData?: ClawSkillSet | null
  onSubmit: (name: string, spec: ClawSkillSetSpec) => Promise<void>
}

export default function SkillSetForm({ initialData, onSubmit }: SkillSetFormProps) {
  const isEdit = !!initialData
  const [name, setName] = useState(initialData?.metadata.name ?? '')
  const [skills, setSkills] = useState<SkillEntry[]>(initialData?.spec.skills ?? [])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const updateSkill = (idx: number, patch: Partial<SkillEntry>) => {
    const next = [...skills]
    next[idx] = { ...next[idx], ...patch }
    setSkills(next)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await onSubmit(name, { skills })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Submit failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="mx-auto max-w-2xl space-y-6">
      <h1 className="text-2xl font-bold text-claw-accent">{isEdit ? 'Edit Skill Set' : 'Create Skill Set'}</h1>

      {error && <div className="rounded border border-claw-error/30 bg-claw-error/10 p-3 text-sm text-claw-error">{error}</div>}

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <FormField label="Name" htmlFor="name">
          <input id="name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} required />
        </FormField>
      </div>

      <div className="rounded-lg border border-claw-border bg-claw-card p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-claw-dim">Skills</h2>
          <button type="button" onClick={() => setSkills([...skills, { name: '', content: '' }])} className="rounded bg-claw-accent/20 px-3 py-1 text-xs text-claw-accent hover:bg-claw-accent/30">
            + Add Skill
          </button>
        </div>
        {skills.map((skill, idx) => (
          <div key={idx} className="mb-4 rounded border border-claw-border bg-claw-bg p-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs text-claw-dim">Skill {idx + 1}</span>
              <RemoveButton onClick={() => setSkills(skills.filter((_, i) => i !== idx))} />
            </div>
            <FormField label="Skill Name" htmlFor={`skill-name-${idx}`}>
              <input id={`skill-name-${idx}`} className={inputClass} value={skill.name} onChange={(e) => updateSkill(idx, { name: e.target.value })} />
            </FormField>
            <FormField label="Content" htmlFor={`skill-content-${idx}`}>
              <textarea id={`skill-content-${idx}`} className={textareaClass} rows={3} value={skill.content} onChange={(e) => updateSkill(idx, { content: e.target.value })} />
            </FormField>
          </div>
        ))}
        {skills.length === 0 && <p className="py-4 text-center text-sm text-claw-dim">No skills added yet</p>}
      </div>

      <FormActions submitting={submitting} isEdit={isEdit} />
    </form>
  )
}
