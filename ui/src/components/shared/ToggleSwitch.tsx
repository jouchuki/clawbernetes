interface ToggleSwitchProps {
  label: string
  enabled: boolean
  onChange: (enabled: boolean) => void
}

export default function ToggleSwitch({ label, enabled, onChange }: ToggleSwitchProps) {
  return (
    <div className="mb-4 flex items-center gap-3">
      <label className="text-sm text-claw-dim">{label}</label>
      <button
        type="button"
        className={`relative h-6 w-11 rounded-full transition-colors ${enabled ? 'bg-claw-accent' : 'bg-claw-border'}`}
        onClick={() => onChange(!enabled)}
      >
        <span className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${enabled ? 'translate-x-5' : ''}`} />
      </button>
    </div>
  )
}
