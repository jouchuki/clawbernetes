import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'

interface UseResourceResult<T> {
  data: T | null
  loading: boolean
  error: string | null
}

export function useResource<T>(path: string): UseResourceResult<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    apiFetch<T>(path)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [path])

  return { data, loading, error }
}
