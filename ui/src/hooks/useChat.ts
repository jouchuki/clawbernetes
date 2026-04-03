import { useState } from 'react'
import { apiFetch } from '../api/client'

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  timestamp: Date
}

export function useChat(agentName: string) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [sending, setSending] = useState(false)

  const send = async (content: string) => {
    setSending(true)
    setMessages((prev) => [
      ...prev,
      { role: 'user', content, timestamp: new Date() },
    ])
    try {
      const res = await apiFetch<{ content: string }>(
        `/api/agents/${agentName}/chat`,
        {
          method: 'POST',
          body: JSON.stringify({ role: 'user', content }),
        },
      )
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: res.content, timestamp: new Date() },
      ])
    } catch (err) {
      setMessages((prev) => [
        ...prev,
        {
          role: 'assistant',
          content: `Error: ${err}`,
          timestamp: new Date(),
        },
      ])
    } finally {
      setSending(false)
    }
  }

  return { messages, send, sending }
}
