import type { MouseEvent, ReactNode } from 'react'
import { Browser } from '@wailsio/runtime'

interface Props {
  href: string
  className?: string
  title?: string
  children: ReactNode
}

// De Wails-webview negeert target="_blank"; externe links moeten via
// Browser.OpenURL in de standaardbrowser geopend worden.
export default function ExternalLink({ href, className, title, children }: Props) {
  const handleClick = (e: MouseEvent<HTMLAnchorElement>) => {
    e.preventDefault()
    e.stopPropagation()
    Browser.OpenURL(href).catch(() => {})
  }

  return (
    <a href={href} title={title} className={className} onClick={handleClick}>
      {children}
    </a>
  )
}
