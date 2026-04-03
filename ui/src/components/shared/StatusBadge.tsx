interface StatusBadgeProps {
  phase: string
}

const phaseStyles: Record<string, string> = {
  Running: 'bg-claw-accent/20 text-claw-accent',
  Pending: 'bg-claw-warn/20 text-claw-warn',
  Error: 'bg-claw-error/20 text-claw-error',
  Hibernating: 'bg-claw-dim/20 text-claw-dim',
}

export default function StatusBadge({ phase }: StatusBadgeProps) {
  const style = phaseStyles[phase] ?? 'bg-claw-dim/20 text-claw-dim'
  return (
    <span
      className={`inline-block rounded-full px-2.5 py-0.5 text-xs font-semibold uppercase tracking-wide ${style}`}
    >
      {phase || 'Unknown'}
    </span>
  )
}
