import { useState } from 'react'
import { Clipboard } from '@wailsio/runtime'

interface Props {
  fout: string
  className?: string
}

// leesbaar pelt de JSON-envelop van een Wails-fout af: over de bridge komt
// {"message":"…","cause":{},"kind":"RuntimeError"}, en dat hoort een mens niet
// rauw te zien.
function leesbaar(fout: string): string {
  const tekst = fout.trim()
  if (tekst.startsWith('{')) {
    try {
      const j = JSON.parse(tekst)
      if (j && typeof j.message === 'string') return j.message
    } catch {
      // geen JSON: dan is het al een gewone boodschap
    }
  }
  return tekst
}

// reHerstelCmd herkent het herstelcommando dat de backend in een
// hostsleutel-melding meestuurt, inclusief variabel [ip]:poort en known_hosts-pad.
const reHerstelCmd = /ssh-keygen -R "[^"]+"(?: -f "[^"]+")?/

// Foutvak toont een fout zoals een mens hem wil lezen. Zit er een uitvoerbaar
// commando in (zoals ssh-keygen -R bij een veranderde hostsleutel), dan komt dat
// apart in een monospace-blok met een kopieerknop — overtypen uit een foutmelding
// is precies waar tikfouten vandaan komen.
export default function Foutvak({ fout, className = '' }: Props) {
  const [gekopieerd, setGekopieerd] = useState(false)

  const boodschap = leesbaar(fout)
  const cmd = boodschap.match(reHerstelCmd)?.[0] ?? null
  const tekst = cmd
    ? boodschap.replace(cmd, '').replace(/\n{3,}/g, '\n\n').trim()
    : boodschap

  const kopieer = async () => {
    if (!cmd) return
    try {
      await Clipboard.SetText(cmd)
      setGekopieerd(true)
      setTimeout(() => setGekopieerd(false), 2000)
    } catch {
      // klembord niet beschikbaar: de tekst blijft selecteerbaar in beeld
    }
  }

  return (
    <div className={`bg-red-soft text-red px-3 py-2 rounded-lg text-[11.5px] ${className}`}>
      <div className="whitespace-pre-wrap leading-relaxed">{tekst}</div>
      {cmd && (
        <div className="mt-2 flex items-center gap-2 bg-panel border border-border rounded-lg px-2.5 py-1.5">
          <code className="font-mono text-[11.5px] text-fg flex-1 select-all break-all">{cmd}</code>
          <button onClick={kopieer}
            className="shrink-0 text-[11px] font-semibold text-fg-muted border border-border rounded-md px-2 py-1 hover:bg-hover hover:text-fg transition">
            {gekopieerd ? '✓ gekopieerd' : 'Kopieer'}
          </button>
        </div>
      )}
    </div>
  )
}
