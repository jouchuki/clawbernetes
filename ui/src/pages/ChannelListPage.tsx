import { useNavigate } from 'react-router-dom'
import { useResourceList } from '../hooks/useResourceList'
import { deleteChannel } from '../api/channels'
import type { ClawChannel } from '../api/types'
import ResourceTable from '../components/shared/ResourceTable'
import type { Column } from '../components/shared/ResourceTable'
import StatusBadge from '../components/shared/StatusBadge'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

const columns: Column<ClawChannel>[] = [
  { key: 'metadata.name', label: 'Name' },
  { key: 'spec.type', label: 'Type' },
  { key: 'spec.enabled', label: 'Enabled', render: (c) => <StatusBadge phase={c.spec.enabled !== false ? 'Running' : 'Hibernating'} /> },
  { key: 'spec.credentialsSecret', label: 'Credentials Secret' },
]

export default function ChannelListPage() {
  const { items, loading, error, refresh } = useResourceList<ClawChannel>('/api/channels')
  const navigate = useNavigate()

  if (loading && items.length === 0) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-claw-accent">Channels</h1>
        <button onClick={() => navigate('/channels/new')} className="rounded bg-claw-accent px-4 py-2 text-sm font-medium text-claw-bg hover:bg-claw-accent/80">
          + Create Channel
        </button>
      </div>
      <ResourceTable
        items={items}
        columns={columns}
        onEdit={(c) => navigate(`/channels/${c.metadata.name}/edit`)}
        onDelete={(c) => deleteChannel(c.metadata.name).then(refresh)}
      />
    </div>
  )
}
