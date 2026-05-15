import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { AppSettings } from '../../bindings/github.com/rdm/sites-tool/internal/services'

interface Props {
  onClose: () => void
}

export default function SettingsPage({ onClose }: Props) {
  const [settings, setSettings] = useState<AppSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showApiKey, setShowApiKey] = useState(false)

  useEffect(() => {
    Services.SettingsService.Get().then(s => setSettings(s)).catch(() => {})
  }, [])

  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => {
    setSettings(prev => prev ? { ...prev, [key]: value } : prev)
    setSaved(false)
  }

  const save = async () => {
    if (!settings) return
    setSaving(true)
    setError(null)
    try {
      await Services.SettingsService.Save(settings)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!settings) {
    return (
      <div className="flex-1 flex items-center justify-center text-gray-600 text-sm">
        <span className="animate-spin inline-block mr-2">↻</span> Laden…
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden">
      {/* Header */}
      <div className="px-6 py-4 border-b border-black/[0.08] shrink-0 flex items-center gap-3">
        <h2 className="text-base font-semibold text-gray-900 flex-1">Instellingen</h2>
        <button
          onClick={onClose}
          className="text-gray-600 hover:text-gray-900 transition-colors text-lg leading-none"
          title="Sluiten"
        >✕</button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-7">

        {/* Kinsta */}
        <section>
          <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-600 mb-3">
            Kinsta
          </h3>
          <div className="bg-black/[0.03] rounded-xl divide-y divide-black/[0.07]">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-xs text-gray-600 w-28 shrink-0">API Key</label>
              <div className="flex-1 flex items-center gap-2">
                <input
                  type={showApiKey ? 'text' : 'password'}
                  value={settings.kinstaApiKey}
                  onChange={e => update('kinstaApiKey', e.target.value)}
                  placeholder="Bearer sk_live_…"
                  className="flex-1 bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                             rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                             font-mono border border-transparent focus:border-indigo-400 min-w-0"
                />
                <button
                  onClick={() => setShowApiKey(v => !v)}
                  className="text-gray-600 hover:text-gray-800 text-xs shrink-0 transition-colors px-1"
                  title={showApiKey ? 'Verbergen' : 'Tonen'}
                >
                  {showApiKey ? '🙈' : '👁'}
                </button>
              </div>
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-xs text-gray-600 w-28 shrink-0">Company ID</label>
              <input
                type="text"
                value={settings.kinstaCompanyId}
                onChange={e => update('kinstaCompanyId', e.target.value)}
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                className="flex-1 bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                           rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                           font-mono border border-transparent focus:border-indigo-400"
              />
            </div>
            <div className="px-4 py-2">
              <p className="text-[11px] text-gray-600">
                De API key en Company ID zijn te vinden in het{' '}
                <span className="text-gray-600">Kinsta dashboard → My Info → API Keys</span>.
              </p>
            </div>
          </div>
        </section>

        {/* Editor */}
        <section>
          <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-600 mb-3">
            Editor
          </h3>
          <div className="bg-black/[0.03] rounded-xl px-4 py-3 flex items-center gap-4">
            <label className="text-xs text-gray-600 w-28 shrink-0">Standaard editor</label>
            <div className="flex gap-2">
              {(['cursor', 'vscode', 'phpstorm'] as const).map(e => (
                <button
                  key={e}
                  onClick={() => update('editor', e)}
                  className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors border
                    ${settings.editor === e
                      ? 'bg-indigo-100 border-indigo-400 text-gray-900'
                      : 'border-black/[0.10] text-gray-600 hover:text-gray-800 hover:bg-black/[0.05]'}`}
                >
                  {e === 'cursor' ? 'Cursor' : e === 'vscode' ? 'VS Code' : 'PhpStorm'}
                </button>
              ))}
            </div>
          </div>
        </section>

        {/* Git */}
        <section>
          <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-600 mb-3">
            Git
          </h3>
          <div className="bg-black/[0.03] rounded-xl divide-y divide-black/[0.07]">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-xs text-gray-600 w-28 shrink-0">Default remote</label>
              <input
                type="text"
                value={settings.gitDefaultRemote}
                onChange={e => update('gitDefaultRemote', e.target.value)}
                placeholder="origin"
                className="w-32 bg-black/[0.05] text-sm text-gray-800 placeholder-gray-400
                           rounded-lg px-3 py-1.5 outline-none focus:ring-1 focus:ring-indigo-500/40
                           font-mono border border-transparent focus:border-indigo-400"
              />
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-xs text-gray-600 w-28 shrink-0">Prune on fetch</label>
              <button
                onClick={() => update('gitPruneOnFetch', !settings.gitPruneOnFetch)}
                className={`relative w-9 h-5 rounded-full transition-colors ${
                  settings.gitPruneOnFetch ? 'bg-indigo-500' : 'bg-black/[0.08]'
                }`}
              >
                <span className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow transition-transform ${
                  settings.gitPruneOnFetch ? 'translate-x-4' : 'translate-x-0'
                }`} />
              </button>
              <span className="text-xs text-gray-600">
                Verwijder remote-tracking branches die niet meer bestaan
              </span>
            </div>
          </div>
        </section>

        {/* Config file location */}
        <section>
          <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-600 mb-3">
            Configuratie
          </h3>
          <div className="bg-black/[0.03] rounded-xl px-4 py-3">
            <p className="text-[11px] text-gray-600">
              Instellingen worden opgeslagen in{' '}
              <code className="text-gray-600 font-mono">~/.config/rdm/config.yml</code>
            </p>
          </div>
        </section>
      </div>

      {/* Footer */}
      <div className="px-6 py-3 border-t border-black/[0.08] shrink-0 flex items-center gap-3">
        {error && (
          <p className="text-xs text-red-600 flex-1 truncate">{error}</p>
        )}
        {saved && !error && (
          <p className="text-xs text-emerald-600 flex-1">✓ Opgeslagen</p>
        )}
        {!error && !saved && <span className="flex-1" />}
        <button
          onClick={onClose}
          className="px-4 py-1.5 text-sm text-gray-600 hover:text-gray-900 transition-colors"
        >
          Annuleren
        </button>
        <button
          onClick={save}
          disabled={saving}
          className="px-4 py-1.5 bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50
                     text-gray-900 text-sm font-medium rounded-lg transition-colors flex items-center gap-2"
        >
          {saving && <span className="animate-spin inline-block text-xs">↻</span>}
          Opslaan
        </button>
      </div>
    </div>
  )
}
