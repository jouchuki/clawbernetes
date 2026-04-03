import type { ChatMessage as ChatMessageType } from '../../hooks/useChat'

interface ChatMessageProps {
  message: ChatMessageType
}

export default function ChatMessage({ message }: ChatMessageProps) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className="max-w-[75%]">
        <div
          className={`rounded-xl px-4 py-2 text-sm whitespace-pre-wrap ${
            isUser
              ? 'bg-claw-accent text-claw-bg rounded-br-sm'
              : 'bg-claw-card border border-claw-border text-claw-text rounded-bl-sm'
          }`}
        >
          {message.content}
        </div>
        <p
          className={`mt-1 text-xs text-claw-dim ${isUser ? 'text-right' : 'text-left'}`}
        >
          {message.timestamp.toLocaleTimeString()}
        </p>
      </div>
    </div>
  )
}
