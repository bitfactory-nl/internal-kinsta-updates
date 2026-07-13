import React from 'react'
import ReactDOM from 'react-dom/client'
import './styles/globals.css'
import App from './App'

// Volg het systeemthema; het design levert tokens voor licht én donker.
const applyTheme = (dark: boolean) =>
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
const mq = window.matchMedia('(prefers-color-scheme: dark)')
applyTheme(mq.matches)
mq.addEventListener('change', e => applyTheme(e.matches))

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
