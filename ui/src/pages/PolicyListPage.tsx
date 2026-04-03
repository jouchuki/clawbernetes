import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deletePolicy } from '../api/policies'
import type { ClawPolicy } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawPolicy>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'spec.budget.daily', label: 'Daily Budget', render: (p) => p.spec.budget?.daily != null ? `$${p.spec.budget.daily}` : '-' },
  { key: 'spec.budget.monthly', label: 'Monthly Budget', render: (p) => p.spec.budget?.monthly != null ? `$${p.spec.budget.monthly}` : '-' },
  { key: 'spec.toolPolicy.deny', label: 'Deny Count', render: (p) => String(p.spec.toolPolicy?.deny?.length ?? 0) },
]

export default function PolicyListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawPolicy>('/api/policies')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Policies</h1>
        <button onClick={() => navigate('/policies/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Policy
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(p) => navigate(`/policies/${p.metadata.name}/edit`)}
        onDelete={(p) => deletePolicy(p.metadata.name).then(refresh)}
      />
    </div>
  )
}
