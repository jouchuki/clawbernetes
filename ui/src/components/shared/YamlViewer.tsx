import { useMemo, useState } from 'react'

interface YamlViewerProps {
  data: unknown
}

export default function YamlViewer({ data }: YamlViewerProps) {
  const [copied, setCopied] = useState(false)
  const text = useMemo(() => JSON.stringify(data, null, 2), [data])

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="relative">
      <button
        onClick={handleCopy}
        className="absolute top-3 right-3 rounded px-3 py-1 text-xs bg-claw-card text-claw-dim hover:text-claw-text border border-claw-border"
      >
        {copied ? 'Copied!' : 'Copy'}
      </button>
      <pre className="overflow-auto rounded-lg border border-claw-border bg-claw-bg p-4 font-mono text-sm text-claw-text">
        {text}
      </pre>
    </div>
  )
}
