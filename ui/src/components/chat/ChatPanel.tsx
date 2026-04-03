import { useEffect, useRef, useState } from 'react'
import { useChat } from '../../hooks/useChat'
import ChatMessage from './ChatMessage'

interface ChatPanelProps {
  agentName: string
}

export default function ChatPanel({ agentName }: ChatPanelProps) {
  const { messages, send, sending } = useChat(agentName)
  const [input, setInput] = useState('')
  const listRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight
    }
  }, [messages])

  const handleSend = () => {
    const text = input.trim()
    if (!text || sending) return
    setInput('')
    send(text)
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value)
    const el = e.target
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 96)}px`
  }

  return (
    <div className="flex flex-col h-[600px]">
      <div ref={listRef} className="flex-1 overflow-auto space-y-4 p-4">
        {messages.length === 0 && (
          <p className="text-center text-claw-dim text-sm">
            Send a message to start chatting with the agent.
          </p>
        )}
        {messages.map((msg, i) => (
          <ChatMessage key={i} message={msg} />
        ))}
        {sending && (
          <div className="flex justify-start">
            <span className="text-sm text-claw-dim animate-pulse">
              Sending...
            </span>
          </div>
        )}
      </div>

      <div className="border-t border-claw-border p-3 flex gap-2 items-end">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          placeholder="Type a message..."
          rows={1}
          className="flex-1 resize-none rounded-lg border border-claw-border bg-claw-bg px-3 py-2 text-sm text-claw-text placeholder:text-claw-dim focus:outline-none focus:border-claw-accent"
        />
        <button
          onClick={handleSend}
          disabled={sending || !input.trim()}
          className="rounded-lg bg-claw-accent px-4 py-2 text-sm font-semibold text-claw-bg disabled:opacity-40"
        >
          {sending ? '...' : 'Send'}
        </button>
      </div>
    </div>
  )
}
