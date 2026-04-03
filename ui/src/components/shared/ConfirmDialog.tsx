interface ConfirmDialogProps {
  name: string
  onConfirm: () => void
  onCancel: () => void
}

export default function ConfirmDialog({ name, onConfirm, onCancel }: ConfirmDialogProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-sm rounded-lg border border-claw-border bg-claw-card p-6">
        <h3 className="mb-2 text-lg font-semibold text-claw-text">Confirm Delete</h3>
        <p className="mb-6 text-sm text-claw-dim">
          Are you sure you want to delete <span className="font-medium text-claw-text">{name}</span>?
        </p>
        <div className="flex justify-end gap-3">
          <button
            onClick={onCancel}
            className="rounded px-4 py-2 text-sm text-claw-text hover:bg-claw-border/40"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="rounded bg-claw-error px-4 py-2 text-sm font-medium text-white hover:bg-claw-error/80"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  )
}
