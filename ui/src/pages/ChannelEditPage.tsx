import { useNavigate, useParams } from 'react-router-dom'
import { useResource } from '../hooks/useResource'
import { updateChannel } from '../api/channels'
import type { ClawChannel, ClawChannelSpec } from '../api/types'
import ChannelForm from '../components/channels/ChannelForm'
import LoadingSpinner from '../components/shared/LoadingSpinner'
import ErrorAlert from '../components/shared/ErrorAlert'

export default function ChannelEditPage() {
  const { name } = useParams()
  const { data, loading, error } = useResource<ClawChannel>(`/api/channels/${name}`)
  const navigate = useNavigate()

  const handleSubmit = async (_name: string, spec: ClawChannelSpec) => {
    await updateChannel(name!, spec)
    navigate('/channels')
  }

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorAlert message={error} />

  return <ChannelForm initialData={data} onSubmit={handleSubmit} />
}
