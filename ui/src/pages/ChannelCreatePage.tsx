import { useNavigate } from 'react-router-dom'
import { createChannel } from '../api/channels'
import ChannelForm from '../components/channels/ChannelForm'
import type { ClawChannelSpec } from '../api/types'

export default function ChannelCreatePage() {
  const navigate = useNavigate()
  const handleSubmit = async (name: string, spec: ClawChannelSpec) => {
    await createChannel({ name, spec })
    navigate('/channels')
  }
  return <ChannelForm onSubmit={handleSubmit} />
}
