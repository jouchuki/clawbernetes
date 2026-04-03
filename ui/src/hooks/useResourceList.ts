import { useState, useEffect, useCallback, useRef } from 'react'
import { apiFetch } from '../api/client'

interface UseResourceListResult<T> {
  items: T[]
  loading: boolean
  error: string | null
  refresh: () => void
}

export function useResourceList<T>(path: string): UseResourceListResult<T> {
  const [items, setItems] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const pathRef = useRef(path)
  pathRef.current = path

  const refresh = useCallback(() => {
    apiFetch<T[]>(pathRef.current)
      .then((data) => {
        setItems(data ?? [])
        setError(null)
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    setLoading(true)
    refresh()
    const id = setInterval(refresh, 5000)
    return () => clearInterval(id)
  }, [refresh])

  return { items, loading, error, refresh }
}
