interface FormActionsProps {
  submitting: boolean
  isEdit: boolean
}

export default function FormActions({ submitting, isEdit }: FormActionsProps) {
  return (
    <div className="flex justify-end gap-3 pt-4">
      <button type="button" onClick={() => window.history.back()} className="rounded px-4 py-2 text-sm text-claw-text hover:bg-claw-border/40">
        Cancel
      </button>
      <button type="submit" disabled={submitting} className="rounded bg-claw-accent px-6 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80 disabled:opacity-50">
        {submitting ? 'Saving...' : isEdit ? 'Update' : 'Create'}
      </button>
    </div>
  )
}
