import { useState, useEffect } from 'react'
import * as Services from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { AppSettings } from '../../bindings/github.com/rdm/sites-tool/internal/services'
import type { UpdateStatus } from '../../bindings/github.com/rdm/sites-tool/internal/domain/models'

interface Props {
  onClose: () => void
}

export default function SettingsPage({ onClose }: Props) {
  const [settings, setSettings] = useState<AppSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showApiKey, setShowApiKey] = useState(false)
  const [showGithubToken, setShowGithubToken] = useState(false)
  const [showAnthropicKey, setShowAnthropicKey] = useState(false)
  const [showWordfenceKey, setShowWordfenceKey] = useState(false)
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [controleren, setControleren] = useState(false)
  const [updateLog, setUpdateLog] = useState<string | null>(null)
  const [showUpdatesToken, setShowUpdatesToken] = useState(false)

  useEffect(() => {
    Services.SettingsService.Get().then(s => setSettings(s)).catch(() => {})
    Services.UpdateService.Status().then(s => setUpdateStatus(s)).catch(() => {})
  }, [])

  const controleerNu = async () => {
    setControleren(true)
    try {
      setUpdateStatus(await Services.UpdateService.Check())
    } catch {
      // De foutmelding komt via Status().lastError terug.
      try { setUpdateStatus(await Services.UpdateService.Status()) } catch { /* stil */ }
    } finally {
      setControleren(false)
    }
  }

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

  const inputClass = `flex-1 bg-bg border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg
                      placeholder-fg-faint outline-none focus:border-accent focus:ring-1 focus:ring-accent/30
                      font-mono min-w-0`

  if (!settings) {
    return (
      <div className="flex-1 flex items-center justify-center text-fg-faint text-[13px]">
        <span className="animate-spin inline-block mr-2">↻</span> Laden…
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden bg-bg">
      {/* Header */}
      <div className="h-14 px-6 bg-panel border-b border-border shrink-0 flex items-center gap-3">
        <h2 className="text-[15px] font-bold text-fg flex-1">Instellingen</h2>
        <button
          onClick={onClose}
          className="text-fg-muted hover:text-fg transition-colors text-lg leading-none"
          title="Sluiten"
        >✕</button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-7 max-w-[820px]">

        {/* Kinsta */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Kinsta
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">API Key</label>
              <div className="flex-1 flex items-center gap-2">
                <input
                  type={showApiKey ? 'text' : 'password'}
                  value={settings.kinstaApiKey}
                  onChange={e => update('kinstaApiKey', e.target.value)}
                  placeholder="Bearer sk_live_…"
                  className={inputClass}
                />
                <button
                  onClick={() => setShowApiKey(v => !v)}
                  className="text-fg-muted hover:text-fg text-xs shrink-0 transition-colors px-1"
                  title={showApiKey ? 'Verbergen' : 'Tonen'}
                >
                  {showApiKey ? '🙈' : '👁'}
                </button>
              </div>
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Company ID</label>
              <input
                type="text"
                value={settings.kinstaCompanyId}
                onChange={e => update('kinstaCompanyId', e.target.value)}
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                className={inputClass}
              />
            </div>
            <div className="px-4 py-2.5">
              <p className="text-[11.5px] text-fg-muted">
                De API key en Company ID zijn te vinden in het{' '}
                <span className="text-fg">Kinsta dashboard → My Info → API Keys</span>.
              </p>
            </div>
          </div>
        </section>

        {/* Wordfence */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Wordfence
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">API Key</label>
              <div className="flex-1 flex items-center gap-2">
                <input
                  type={showWordfenceKey ? 'text' : 'password'}
                  value={settings.wordfenceApiKey}
                  onChange={e => update('wordfenceApiKey', e.target.value)}
                  placeholder="wfi_…"
                  className={inputClass}
                />
                <button
                  onClick={() => setShowWordfenceKey(v => !v)}
                  className="text-fg-muted hover:text-fg text-xs shrink-0 transition-colors px-1"
                  title={showWordfenceKey ? 'Verbergen' : 'Tonen'}
                >
                  {showWordfenceKey ? '🙈' : '👁'}
                </button>
              </div>
            </div>
            <div className="px-4 py-2.5">
              <p className="text-[11.5px] text-fg-muted">
                Wordfence Intelligence → API-key voor de{' '}
                <span className="text-fg">production</span> vulnerability-feed.
              </p>
            </div>
          </div>
        </section>

        {/* GitHub */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            GitHub
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Access token</label>
              <div className="flex-1 flex items-center gap-2">
                <input
                  type={showGithubToken ? 'text' : 'password'}
                  value={settings.githubToken}
                  onChange={e => update('githubToken', e.target.value)}
                  placeholder="ghp_… of github_pat_…"
                  className={inputClass}
                />
                <button
                  onClick={() => setShowGithubToken(v => !v)}
                  className="text-fg-muted hover:text-fg text-xs shrink-0 transition-colors px-1"
                  title={showGithubToken ? 'Verbergen' : 'Tonen'}
                >
                  {showGithubToken ? '🙈' : '👁'}
                </button>
              </div>
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Plugin-repo</label>
              <input
                value={settings.pluginRepo}
                onChange={e => update('pluginRepo', e.target.value)}
                placeholder="org/repo-naam"
                className={inputClass}
              />
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Branch/ref</label>
              <input
                value={settings.pluginRepoRef}
                onChange={e => update('pluginRepoRef', e.target.value)}
                placeholder="main"
                className={inputClass}
              />
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Lokale map</label>
              <input
                value={settings.pluginLocalDir}
                onChange={e => update('pluginLocalDir', e.target.value)}
                placeholder="/Users/naam/Downloads/plugin-updates (tijdelijk, naast de repo)"
                className={inputClass}
              />
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Referentie-installatie</label>
              <input
                value={settings.pluginReferencePath}
                onChange={e => update('pluginReferencePath', e.target.value)}
                placeholder="/Users/naam/Projects/internal-wordpress-paid-plugins"
                className={inputClass}
              />
            </div>
            <div className="px-4 py-2.5">
              <p className="text-[11.5px] text-fg-muted">
                Personal access token met leestoegang tot de bitfactory-nl repositories
                (scopes: <span className="font-mono text-fg">repo</span> + Actions read).
                Gebruikt voor de Security- en Plugins-tab. De plugin-repo
                (bijv. <span className="font-mono text-fg">bitfactory-nl/paid-plugins</span>) bevat
                het manifest met betaalde plugins voor de Plugins-tab.
              </p>
              <p className="text-[11.5px] text-fg-muted mt-1.5">
                De referentie-installatie is een lokale WordPress-checkout die je zelf actueel
                houdt met alle betaalde plugins erin. Dit project wordt uit het Plugins-overzicht
                in het hoofdmenu gehouden — het is geen klantsite — maar bepaalt daar wél de
                "laatste versie" voor elke plugin die erin staat (ook als wp.org toevallig een
                andere versie kent), en is de bron waarvandaan je plugins naar projecten kunt
                bijwerken.
              </p>
            </div>
          </div>
        </section>

        {/* AI (Claude) */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            AI (Claude)
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Anthropic API Key</label>
              <div className="flex-1 flex items-center gap-2">
                <input
                  type={showAnthropicKey ? 'text' : 'password'}
                  value={settings.anthropicApiKey}
                  onChange={e => update('anthropicApiKey', e.target.value)}
                  placeholder="sk-ant-…"
                  className={inputClass}
                />
                <button
                  onClick={() => setShowAnthropicKey(v => !v)}
                  className="text-fg-muted hover:text-fg text-xs shrink-0 transition-colors px-1"
                  title={showAnthropicKey ? 'Verbergen' : 'Tonen'}
                >
                  {showAnthropicKey ? '🙈' : '👁'}
                </button>
              </div>
            </div>
            <div className="px-4 py-2.5">
              <p className="text-[11.5px] text-fg-muted">
                Wordt gebruikt door de <span className="text-fg">Tests</span>-tab (AI visuele
                vergelijking, flow-authoring en self-heal). Maak een key aan in de{' '}
                <span className="text-fg">Anthropic Console → API Keys</span>. Opgeslagen in{' '}
                <code className="text-fg font-mono">~/.config/rdm/config.yml</code> (alleen leesbaar
                voor jouw gebruiker), net als de andere keys.
              </p>
            </div>
          </div>
        </section>

        {/* Editor */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Editor
          </h3>
          <div className="bg-panel border border-border rounded-[11px] px-4 py-3 flex items-center gap-4">
            <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Standaard editor</label>
            <div className="flex gap-2">
              {(['cursor', 'vscode', 'phpstorm'] as const).map(e => (
                <button
                  key={e}
                  onClick={() => update('editor', e)}
                  className={`px-3 py-1.5 rounded-[9px] text-[12.5px] font-semibold transition-colors border
                    ${settings.editor === e
                      ? 'bg-accent-soft border-accent text-accent'
                      : 'bg-panel-2 border-border text-fg-muted hover:bg-hover'}`}
                >
                  {e === 'cursor' ? 'Cursor' : e === 'vscode' ? 'VS Code' : 'PhpStorm'}
                </button>
              ))}
            </div>
          </div>
        </section>

        {/* Database */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Database
          </h3>
          <div className="bg-panel border border-border rounded-[11px] px-4 py-3 flex items-center gap-4">
            <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Database-programma</label>
            <input
              value={settings.dbApp}
              onChange={e => update('dbApp', e.target.value)}
              placeholder="Sequel Ace"
              className={inputClass}
            />
          </div>
        </section>

        {/* Git */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Git
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Default remote</label>
              <input
                type="text"
                value={settings.gitDefaultRemote}
                onChange={e => update('gitDefaultRemote', e.target.value)}
                placeholder="origin"
                className="w-36 bg-bg border border-border rounded-[9px] px-3 py-2 text-[13px] text-fg
                           placeholder-fg-faint outline-none focus:border-accent focus:ring-1 focus:ring-accent/30
                           font-mono"
              />
            </div>
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Prune on fetch</label>
              <button
                onClick={() => update('gitPruneOnFetch', !settings.gitPruneOnFetch)}
                className={`relative w-9 h-5 rounded-full transition-colors ${
                  settings.gitPruneOnFetch ? 'bg-accent' : 'bg-panel-2 border border-border'
                }`}
              >
                <span className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow transition-transform ${
                  settings.gitPruneOnFetch ? 'translate-x-4' : 'translate-x-0'
                }`} />
              </button>
              <span className="text-[12.5px] text-fg-muted">
                Verwijder remote-tracking branches die niet meer bestaan
              </span>
            </div>
          </div>
        </section>

        {/* App-updates */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            App-updates
          </h3>
          <div className="bg-panel border border-border rounded-[11px] divide-y divide-border">
            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Huidige versie</label>
              <span className="text-[12.5px] text-fg font-mono flex-1">
                {updateStatus?.currentVersion || '—'}
              </span>
              <button
                onClick={controleerNu}
                disabled={controleren || !updateStatus?.enabled}
                className="bg-panel-2 border border-border text-fg-muted text-[12px] font-semibold px-3 py-1.5
                           rounded-[8px] hover:bg-hover disabled:opacity-50 transition-colors flex items-center gap-1.5"
              >
                {controleren && <span className="animate-spin inline-block text-[10px]">↻</span>}
                Nu controleren
              </button>
            </div>

            {!updateStatus?.enabled && (
              <div className="px-4 py-3">
                <p className="text-[11.5px] text-fg-faint">
                  Zelf-update staat uit in deze build: hij is lokaal gebouwd of
                  draait niet vanuit een <code className="font-mono">.app</code>.
                </p>
              </div>
            )}

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Laatste check</label>
              <span className="text-[12.5px] text-fg-muted flex-1">
                {updateStatus?.lastCheck && !updateStatus.lastCheck.startsWith('0001-01-01')
                  ? new Date(updateStatus.lastCheck).toLocaleString('nl-NL')
                  : 'nog niet gecontroleerd'}
              </span>
            </div>

            {updateStatus?.available && (
              <div className="px-4 py-3">
                <p className="text-[12.5px] text-fg">
                  Versie <span className="font-mono">{updateStatus.available.version}</span> is beschikbaar
                  {updateStatus.available.skipped ? ' (overgeslagen)' : ''}.
                </p>
              </div>
            )}

            {updateStatus?.lastError && (
              <div className="px-4 py-3">
                <p className="text-[12.5px] text-red">{updateStatus.lastError}</p>
              </div>
            )}

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Automatisch</label>
              <button
                onClick={() => update('updatesAutoCheck', !settings.updatesAutoCheck)}
                className={`relative w-9 h-5 rounded-full transition-colors ${
                  settings.updatesAutoCheck ? 'bg-accent' : 'bg-panel-2 border border-border'
                }`}
              >
                <span className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow transition-transform ${
                  settings.updatesAutoCheck ? 'translate-x-4' : 'translate-x-0'
                }`} />
              </button>
              <span className="text-[12.5px] text-fg-muted">
                Controleer bij het opstarten en daarna elke 6 uur
              </span>
            </div>

            <div className="flex items-center gap-4 px-4 py-3">
              <label className="text-[12.5px] text-fg-muted w-28 shrink-0">Token</label>
              <input
                type={showUpdatesToken ? 'text' : 'password'}
                value={settings.updatesGithubToken}
                onChange={e => update('updatesGithubToken', e.target.value)}
                placeholder="leeg = token van de plugin-repo"
                className={inputClass}
              />
              <button
                onClick={() => setShowUpdatesToken(v => !v)}
                className="text-[11.5px] text-fg-muted hover:text-fg shrink-0"
              >
                {showUpdatesToken ? 'verberg' : 'toon'}
              </button>
            </div>

            <div className="px-4 py-3">
              <button
                onClick={async () => {
                  try {
                    setUpdateLog(await Services.UpdateService.InstallLog() || 'Nog geen update-log.')
                  } catch (e) {
                    setUpdateLog(String(e))
                  }
                }}
                className="text-[11.5px] text-accent hover:underline"
              >
                Toon update-log
              </button>
              {updateLog !== null && (
                <pre className="mt-2 max-h-48 overflow-auto text-[11px] font-mono text-fg-muted
                                bg-bg border border-border rounded-[8px] p-2 whitespace-pre-wrap">
                  {updateLog}
                </pre>
              )}
            </div>
          </div>
        </section>

        {/* Config file location */}
        <section>
          <h3 className="text-[11px] font-semibold tracking-[.08em] text-fg-faint uppercase mb-2.5">
            Configuratie
          </h3>
          <div className="bg-panel border border-border rounded-[11px] px-4 py-3">
            <p className="text-[11.5px] text-fg-muted">
              Instellingen worden opgeslagen in{' '}
              <code className="text-fg font-mono">~/.config/rdm/config.yml</code>
            </p>
          </div>
        </section>
      </div>

      {/* Footer */}
      <div className="px-6 py-3 bg-panel border-t border-border shrink-0 flex items-center gap-3">
        {error && (
          <p className="text-[12.5px] text-red flex-1 truncate">{error}</p>
        )}
        {saved && !error && (
          <p className="text-[12.5px] text-green flex-1">✓ Opgeslagen</p>
        )}
        {!error && !saved && <span className="flex-1" />}
        <button
          onClick={onClose}
          className="bg-panel-2 border border-border text-fg-muted text-[12.5px] font-semibold px-[15px] py-[9px]
                     rounded-[9px] hover:bg-hover transition-colors"
        >
          Annuleren
        </button>
        <button
          onClick={save}
          disabled={saving}
          className="bg-accent text-white text-[12.5px] font-semibold px-[18px] py-[9px] rounded-[9px]
                     hover:bg-accent-2 disabled:opacity-50 transition-colors flex items-center gap-2"
        >
          {saving && <span className="animate-spin inline-block text-xs">↻</span>}
          Opslaan
        </button>
      </div>
    </div>
  )
}
