import type { ReactNode } from 'react'

interface FormFieldProps {
  label: string
  children?: ReactNode
  htmlFor?: string
}

export default function FormField({ label, children, htmlFor }: FormFieldProps) {
  return (
    <div className="mb-4">
      <label htmlFor={htmlFor} className="mb-1 block text-sm font-medium text-claw-dim">
        {label}
      </label>
      {children}
    </div>
  )
}

/* Shared classNames for form inputs */
export const inputClass =
  'w-full rounded border border-claw-border bg-claw-bg px-3 py-2 text-sm text-claw-text placeholder:text-claw-dim/60 focus:border-claw-accent focus:outline-none'

export const selectClass = inputClass

export const textareaClass = `${inputClass} min-h-[80px] resize-y`
