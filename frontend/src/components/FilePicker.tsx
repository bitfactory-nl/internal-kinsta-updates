import { useState, useMemo } from 'react'

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  children: TreeNode[]
}

function buildTree(files: string[]): TreeNode[] {
  const root: TreeNode[] = []

  for (const file of files) {
    const parts = file.split('/')
    let nodes = root
    let cumPath = ''
    for (let i = 0; i < parts.length; i++) {
      cumPath = cumPath ? `${cumPath}/${parts[i]}` : parts[i]
      const isLast = i === parts.length - 1
      let node = nodes.find(n => n.name === parts[i])
      if (!node) {
        node = { name: parts[i], path: cumPath, isDir: !isLast, children: [] }
        nodes.push(node)
      }
      if (!isLast) nodes = node.children
    }
  }

  return sortNodes(root)
}

function sortNodes(nodes: TreeNode[]): TreeNode[] {
  return nodes
    .map(n => ({ ...n, children: sortNodes(n.children) }))
    .sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
      return a.name.localeCompare(b.name)
    })
}

function flattenFiltered(nodes: TreeNode[], query: string): string[] {
  const results: string[] = []
  for (const n of nodes) {
    if (n.isDir) {
      results.push(...flattenFiltered(n.children, query))
    } else if (n.path.toLowerCase().includes(query.toLowerCase())) {
      results.push(n.path)
    }
  }
  return results
}

interface NodeProps {
  node: TreeNode
  depth: number
  expanded: Set<string>
  onToggle: (path: string) => void
  selected: string | null
  onSelect: (path: string) => void
}

function TreeNodeRow({ node, depth, expanded, onToggle, selected, onSelect }: NodeProps) {
  const isExpanded = expanded.has(node.path)
  const indent = depth * 12

  if (node.isDir) {
    return (
      <>
        <button
          onClick={() => onToggle(node.path)}
          className="w-full text-left flex items-center gap-1 px-2 py-0.5 hover:bg-black/[0.04] transition-colors text-gray-600"
          style={{ paddingLeft: 8 + indent }}
        >
          <span className="text-[10px] shrink-0 w-3 text-gray-600">
            {isExpanded ? '▾' : '▸'}
          </span>
          <span className="text-xs truncate">{node.name}</span>
        </button>
        {isExpanded && node.children.map(child => (
          <TreeNodeRow
            key={child.path}
            node={child}
            depth={depth + 1}
            expanded={expanded}
            onToggle={onToggle}
            selected={selected}
            onSelect={onSelect}
          />
        ))}
      </>
    )
  }

  const ext = node.name.includes('.') ? node.name.split('.').pop()! : ''
  const extColor: Record<string, string> = {
    php: 'text-violet-600', js: 'text-yellow-600', ts: 'text-blue-600',
    tsx: 'text-blue-600', jsx: 'text-yellow-600', css: 'text-pink-600',
    scss: 'text-pink-600', html: 'text-orange-600', json: 'text-amber-500',
    md: 'text-gray-600', yml: 'text-emerald-600', yaml: 'text-emerald-600',
  }
  const dotColor = extColor[ext] ?? 'text-gray-600'

  return (
    <button
      onClick={() => onSelect(node.path)}
      className={`w-full text-left flex items-center gap-1.5 px-2 py-0.5 transition-colors
        ${selected === node.path ? 'bg-indigo-100 text-gray-900' : 'hover:bg-black/[0.04] text-gray-700'}`}
      style={{ paddingLeft: 8 + indent }}
    >
      <span className={`text-[10px] shrink-0 ${dotColor}`}>●</span>
      <span className="text-xs truncate font-mono">{node.name}</span>
    </button>
  )
}

interface Props {
  files: string[]
  selected: string | null
  onSelect: (path: string) => void
}

export default function FilePicker({ files, selected, onSelect }: Props) {
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())

  const tree = useMemo(() => buildTree(files), [files])

  const toggle = (path: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  const filteredFiles = search ? flattenFiltered(tree, search) : null

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="px-2 py-1.5 border-b border-black/[0.08] shrink-0">
        <input
          type="search"
          placeholder="Filter bestanden…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="w-full bg-black/[0.05] text-xs text-gray-800 placeholder-gray-400
                     rounded px-2 py-1 outline-none focus:ring-1 focus:ring-indigo-500/40"
        />
      </div>
      <div className="flex-1 overflow-y-auto py-1">
        {filteredFiles ? (
          filteredFiles.length === 0 ? (
            <p className="text-xs text-gray-600 text-center py-4">Geen resultaten</p>
          ) : (
            filteredFiles.map(path => (
              <button
                key={path}
                onClick={() => onSelect(path)}
                className={`w-full text-left px-3 py-0.5 text-xs font-mono transition-colors truncate
                  ${selected === path ? 'bg-indigo-100 text-gray-900' : 'hover:bg-black/[0.04] text-gray-700'}`}
              >
                {path}
              </button>
            ))
          )
        ) : (
          tree.map(node => (
            <TreeNodeRow
              key={node.path}
              node={node}
              depth={0}
              expanded={expanded}
              onToggle={toggle}
              selected={selected}
              onSelect={onSelect}
            />
          ))
        )}
      </div>
    </div>
  )
}
