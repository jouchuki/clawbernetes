interface ErrorAlertProps {
  message: string
}

export default function ErrorAlert({ message }: ErrorAlertProps) {
  return (
    <div className="rounded-lg border border-claw-error/30 bg-claw-error/10 p-4 text-claw-error">
      <p className="text-sm font-medium">Error</p>
      <p className="mt-1 text-sm">{message}</p>
    </div>
  )
}
