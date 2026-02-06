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
      className="px-3 py-2 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
    >
      <option value="">All namespaces</option>
      {namespaces.map((ns) => (
        <option key={ns} value={ns}>{ns}</option>
      ))}
    </select>
  )
}
