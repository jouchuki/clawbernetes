import { useEffect, useState } from 'react'

export function useLogStream(agentName: string, enabled: boolean) {
  const [lines, setLines] = useState<string[]>([])
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    if (!enabled) return

    const es = new EventSource(`/api/agents/${agentName}/logs?follow=true&tailLines=100`)
    es.onopen = () => setConnected(true)
    es.onmessage = (e) =>
      setLines((prev) => [...prev.slice(-500), e.data])
    es.onerror = () => setConnected(false)

    return () => es.close()
  }, [agentName, enabled])

  const clear = () => setLines([])

  return { lines, connected, clear }
}
