import { useState, useEffect, useRef, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { Events } from '@wailsio/runtime'
import '@xterm/xterm/css/xterm.css'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import { SSHTarget } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'
import type { Environment } from '../../bindings/github.com/rdm/sites-tool/internal/adapters/kinsta/models'

interface Props {
  projectId: string
}

function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return bytes
}

export default function SshTerminalTab({ projectId }: Props) {
  const [envs, setEnvs] = useState<Environment[]>([])
  const [selectedEnvId, setSelectedEnvId] = useState<string | null>(null)
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [user, setUser] = useState('')
  const [connected, setConnected] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const sessionRef = useRef<string | null>(null)
  const cleanupRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    setError(null)
    Services.KinstaService.GetLinkedSiteID(projectId)
      .then(id => {
        if (!id) return
        return Services.KinstaService.GetSiteDetails(id).then(site => {
          setEnvs(site?.environments ?? [])
        })
      })
      .catch(e => setError(getErrorMessage(e)))
  }, [projectId])

  const selectEnv = useCallback((env: Environment) => {
    setSelectedEnvId(env.id)
    setHost(env.ssh_connection?.ssh_ip?.external_ip ?? '')
    setPort(env.ssh_connection?.ssh_port ?? '')
  }, [])

  const disconnect = useCallback(() => {
    cleanupRef.current?.()
    cleanupRef.current = null
    const id = sessionRef.current
    sessionRef.current = null
    if (id) {
      Services.SSHService.Close(id).catch(() => {})
    }
    termRef.current?.dispose()
    termRef.current = null
    setConnected(false)
  }, [])

  const connect = useCallback(async () => {
    if (!containerRef.current) return
    if (!user.trim()) {
      setError('Gebruikersnaam is verplicht (zie MyKinsta voor de SSH-gebruiker).')
      return
    }
    setError(null)
    setConnecting(true)
    try {
      const term = new Terminal({
        fontSize: 12,
        fontFamily: 'Menlo, Monaco, monospace',
        cursorBlink: true,
        theme: { background: '#1e1e1e', foreground: '#e5e5e5' },
      })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(containerRef.current)
      fit.fit()
      termRef.current = term

      const target = new SSHTarget({
        host: host.trim(),
        port: parseInt(port, 10) || 22,
        user: user.trim(),
      })
      const id = await Services.SSHService.OpenSession(target, term.cols, term.rows)
      sessionRef.current = id

      const offData = Events.On(`ssh:${id}:data`, ev => {
        const payload = ev.data
        if (typeof payload === 'string') {
          term.write(base64ToBytes(payload))
        }
      })
      const offExit = Events.On(`ssh:${id}:exit`, () => {
        term.write('\r\n\x1b[33m[sessie beëindigd]\x1b[0m\r\n')
        disconnect()
      })
      const inputDisposable = term.onData(d => {
        Services.SSHService.Write(id, d).catch(() => {})
      })

      const ro = new ResizeObserver(() => {
        try {
          fit.fit()
          if (sessionRef.current) {
            Services.SSHService.Resize(sessionRef.current, term.cols, term.rows).catch(() => {})
          }
        } catch {
          // ignore transient resize errors
        }
      })
      ro.observe(containerRef.current)

      cleanupRef.current = () => {
        offData()
        offExit()
        inputDisposable.dispose()
        ro.disconnect()
      }

      setConnected(true)
      term.focus()
    } catch (e: unknown) {
      setError(getErrorMessage(e))
      disconnect()
    } finally {
      setConnecting(false)
    }
  }, [host, port, user, disconnect])

  useEffect(() => () => disconnect(), [disconnect])

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Environment list */}
      <div className="w-[200px] shrink-0 border-r border-black/[0.08] flex flex-col overflow-hidden">
        <div className="px-3 py-2 border-b border-black/[0.06] shrink-0">
          <p className="text-xs font-medium text-gray-900">Omgevingen</p>
        </div>
        <div className="flex-1 overflow-y-auto py-1">
          {envs.length === 0 && (
            <p className="text-xs text-gray-600 italic px-3 py-2">Geen Kinsta-omgevingen gevonden.</p>
          )}
          {envs.map(env => (
            <button
              key={env.id}
              onClick={() => selectEnv(env)}
              disabled={connected}
              className={`w-full text-left px-3 py-2 border-b border-black/[0.06] transition-colors disabled:opacity-50
                ${selectedEnvId === env.id ? 'bg-indigo-100' : 'hover:bg-black/[0.04]'}`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${env.is_blocked ? 'bg-red-400' : 'bg-emerald-400'}`} />
                <span className="text-xs text-gray-800 truncate">{env.display_name || env.name}</span>
                {env.name === 'live' && <span className="ml-auto text-[9px] text-indigo-700 shrink-0">live</span>}
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* Connection form + terminal */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        <div className="px-3 py-2 border-b border-black/[0.06] shrink-0 flex items-end gap-2 flex-wrap">
          <Field label="Host">
            <input value={host} onChange={e => setHost(e.target.value)} disabled={connected}
              className="w-36 text-xs px-2 py-1 rounded border border-black/10 bg-white disabled:opacity-60 font-mono" />
          </Field>
          <Field label="Poort">
            <input value={port} onChange={e => setPort(e.target.value)} disabled={connected}
              className="w-16 text-xs px-2 py-1 rounded border border-black/10 bg-white disabled:opacity-60 font-mono" />
          </Field>
          <Field label="Gebruiker">
            <input value={user} onChange={e => setUser(e.target.value)} disabled={connected} placeholder="ssh-user"
              className="w-32 text-xs px-2 py-1 rounded border border-black/10 bg-white disabled:opacity-60 font-mono" />
          </Field>
          {!connected ? (
            <button onClick={connect} disabled={connecting || !host}
              className="text-xs px-3 py-1 rounded bg-indigo-600 text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors">
              {connecting ? 'Verbinden…' : 'Verbinden'}
            </button>
          ) : (
            <button onClick={disconnect}
              className="text-xs px-3 py-1 rounded bg-red-600 text-white hover:bg-red-500 transition-colors">
              Verbreken
            </button>
          )}
        </div>

        {error && (
          <div className="m-3 bg-red-100 text-red-600 px-3 py-2 rounded text-xs">{error}</div>
        )}

        <div className="flex-1 min-h-0 bg-[#1e1e1e] p-1">
          <div ref={containerRef} className="w-full h-full" />
        </div>
        {!connected && !error && (
          <p className="text-[11px] text-gray-600 px-3 py-1.5 shrink-0">
            Kies een omgeving om host/poort in te vullen, vul de SSH-gebruiker in en verbind. Authenticatie via je ssh-agent.
          </p>
        )}
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-0.5">
      <span className="text-[10px] text-gray-600 uppercase tracking-wide">{label}</span>
      {children}
    </label>
  )
}
