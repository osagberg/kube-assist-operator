interface Props {
  namespaces: string[]
  selected: string
  onChange: (ns: string) => void
  selectRef?: React.RefObject<HTMLSelectElement | null>
}

export function NamespaceFilter({ namespaces, selected, onChange, selectRef }: Props) {
  return (
    <select
      ref={selectRef}
      value={selected}
      onChange={(e) => onChange(e.target.value)}
      className="px-3 py-2 glass-inset rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-accent/60 transition-all duration-200"
      style={{ color: 'var(--text-primary)' }}
    >
      <option value="">All namespaces</option>
      {namespaces.map((ns) => (
        <option key={ns} value={ns}>{ns}</option>
      ))}
    </select>
  )
}
