import { useState } from 'react'
import ConfirmDialog from './ConfirmDialog'

export interface Column<T> {
  key: string
  label: string
  render?: (item: T) => React.ReactNode
}

interface ResourceTableProps<T> {
  items: T[]
  columns: Column<T>[]
  onEdit?: (item: T) => void
  onDelete?: (item: T) => void
  nameKey?: string
}

function getNestedValue(obj: unknown, path: string): unknown {
  return path.split('.').reduce((cur, key) => (cur as Record<string, unknown>)?.[key], obj)
}

export default function ResourceTable<T>({
  items,
  columns,
  onEdit,
  onDelete,
  nameKey = 'metadata.name',
}: ResourceTableProps<T>) {
  const [deleteTarget, setDeleteTarget] = useState<T | null>(null)

  const getName = (item: T) => String(getNestedValue(item, nameKey) ?? '')

  return (
    <>
      <div className="overflow-x-auto rounded-lg border border-claw-border">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b border-claw-border bg-claw-card">
              {columns.map((col) => (
                <th key={col.key} className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-claw-dim">
                  {col.label}
                </th>
              ))}
              {(onEdit || onDelete) && (
                <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-claw-dim">Actions</th>
              )}
            </tr>
          </thead>
          <tbody>
            {items.length === 0 && (
              <tr>
                <td colSpan={columns.length + 1} className="px-4 py-8 text-center text-claw-dim">
                  No resources found
                </td>
              </tr>
            )}
            {items.map((item, i) => (
              <tr key={i} className="border-b border-claw-border bg-claw-card hover:bg-claw-border/20">
                {columns.map((col) => (
                  <td key={col.key} className="px-4 py-3 text-claw-text">
                    {col.render ? col.render(item) : String(getNestedValue(item, col.key) ?? '-')}
                  </td>
                ))}
                {(onEdit || onDelete) && (
                  <td className="px-4 py-3">
                    <div className="flex gap-2">
                      {onEdit && (
                        <button
                          onClick={() => onEdit(item)}
                          className="rounded p-1.5 text-claw-dim hover:bg-claw-border/40 hover:text-claw-accent"
                          title="Edit"
                        >
                          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                          </svg>
                        </button>
                      )}
                      {onDelete && (
                        <button
                          onClick={() => setDeleteTarget(item)}
                          className="rounded p-1.5 text-claw-dim hover:bg-claw-error/20 hover:text-claw-error"
                          title="Delete"
                        >
                          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                          </svg>
                        </button>
                      )}
                    </div>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {deleteTarget && onDelete && (
        <ConfirmDialog
          name={getName(deleteTarget)}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => {
            onDelete(deleteTarget)
            setDeleteTarget(null)
          }}
        />
      )}
    </>
  )
}
