interface PlaceholderPageProps {
  title: string
}

export default function PlaceholderPage({ title }: PlaceholderPageProps) {
  return (
    <div>
      <h1 className="mb-4 text-2xl font-bold text-claw-accent">{title}</h1>
      <p className="text-claw-dim">Coming soon</p>
    </div>
  )
}
