interface RemoveButtonProps {
  onClick: () => void
}

export default function RemoveButton({ onClick }: RemoveButtonProps) {
  return (
    <button type="button" onClick={onClick} className="shrink-0 rounded p-2 text-claw-error hover:bg-claw-error/20">
      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
      </svg>
    </button>
  )
}
