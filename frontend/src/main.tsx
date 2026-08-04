import React from 'react'
import ReactDOM from 'react-dom/client'
import './styles/globals.css'
import App from './App'
import { initTheme } from './lib/thema'

// Thema volgt standaard het systeem; de gebruiker kan licht/donker forceren.
initTheme()

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
