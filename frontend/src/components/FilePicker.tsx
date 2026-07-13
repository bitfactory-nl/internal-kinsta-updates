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

function fileDotClass(name: string): string {
  const lower = name.toLowerCase()
  if (lower.startsWith('dockerfile') || lower.startsWith('docker-compose')) return 'bg-green'
  const ext = lower.includes('.') ? lower.split('.').pop()! : ''
  if (ext === 'php') return 'bg-purple'
  if (['json', 'yml', 'yaml', 'env', 'ini', 'conf', 'lock', 'toml', 'xml'].includes(ext)) return 'bg-orange'
  return 'bg-fg-faint opacity-50'
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
          className="w-full text-left flex items-center gap-2 px-[9px] py-[5px] rounded-md
                     text-[12.5px] font-mono font-[450] text-fg-muted hover:bg-hover transition-colors"
          style={{ paddingLeft: 9 + indent }}
        >
          <span className="text-fg-faint shrink-0">{isExpanded ? '▾' : '▸'}</span>
          <span className="truncate">{node.name}</span>
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

  return (
    <button
      onClick={() => onSelect(node.path)}
      className={`w-full text-left flex items-center gap-[9px] px-[9px] py-[5px] rounded-md
        text-[12.5px] font-mono font-[450] transition-colors
        ${selected === node.path ? 'bg-sel text-fg' : 'text-fg hover:bg-hover'}`}
      style={{ paddingLeft: 9 + indent }}
    >
      <span className={`w-[7px] h-[7px] rounded-full shrink-0 ${fileDotClass(node.name)}`} />
      <span className="truncate">{node.name}</span>
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
      <div className="px-3.5 py-3 border-b border-border shrink-0">
        <input
          type="search"
          placeholder="Filter bestanden…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="w-full bg-bg border border-border rounded-[8px] px-[11px] py-2
                     text-[12.5px] text-fg placeholder-fg-faint outline-none
                     focus:border-accent focus:ring-1 focus:ring-accent/30"
        />
      </div>
      <div className="flex-1 overflow-y-auto py-1.5 px-2">
        {filteredFiles ? (
          filteredFiles.length === 0 ? (
            <p className="text-[12.5px] text-fg-faint italic text-center py-4">Geen resultaten</p>
          ) : (
            filteredFiles.map(path => (
              <button
                key={path}
                onClick={() => onSelect(path)}
                className={`w-full text-left px-[9px] py-[5px] rounded-md text-[12.5px]
                  font-mono font-[450] transition-colors truncate
                  ${selected === path ? 'bg-sel text-fg' : 'text-fg-muted hover:bg-hover'}`}
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
