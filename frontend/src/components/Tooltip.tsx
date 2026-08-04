interface TooltipProps {
  label: string
  children: React.ReactNode
}

// Tooltip toont een tekstballon bij hover, zuiver via CSS (Tailwind's
// group/group-hover). Het native "title"-attribuut is in deze WKWebView-
// gebaseerde Wails-app niet betrouwbaar zichtbaar — vergelijkbaar met het
// probleem dat window.confirm() hier stil faalt (zie lib/bevestig.ts) — dus
// geen JS-state of event-handlers, alleen CSS.
export default function Tooltip({ label, children }: TooltipProps) {
  return (
    <span className="relative inline-flex group">
      {children}
      <span
        className="pointer-events-none absolute left-1/2 top-full z-50 mt-1.5 -translate-x-1/2
                   whitespace-nowrap rounded-md border border-border bg-panel-2 px-2 py-1
                   text-[11px] text-fg shadow opacity-0 transition-opacity duration-150
                   group-hover:opacity-100"
      >
        {label}
      </span>
    </span>
  )
}
