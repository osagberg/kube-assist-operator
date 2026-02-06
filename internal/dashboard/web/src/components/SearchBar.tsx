interface Props {
  value: string
  onChange: (v: string) => void
  inputRef?: React.RefObject<HTMLInputElement | null>
}

export function SearchBar({ value, onChange, inputRef }: Props) {
  return (
    <div className="relative">
      <svg
        className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4"
        style={{ color: 'var(--text-tertiary)' }}
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
      </svg>
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Search issues... (press /)"
        className="w-full pl-10 pr-4 py-2 glass-inset rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-accent/60 focus:border-transparent transition-all duration-200"
        style={{ color: 'var(--text-primary)' }}
      />
    </div>
  )
}
