// Themabeheer: 'system' volgt het OS, 'light' en 'dark' forceren een keuze.
// De tokens voor beide thema's staan in styles/globals.css.

export type ThemeMode = 'system' | 'light' | 'dark'

const KEY = 'kinsta-updater:theme'
const mq = window.matchMedia('(prefers-color-scheme: dark)')

const listeners = new Set<() => void>()
let mode: ThemeMode = read()

function read(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'system' || v === 'light' || v === 'dark') return v
  } catch {
    // localStorage kan geblokkeerd zijn; val terug op het systeemthema.
  }
  return 'system'
}

/** Het thema dat daadwerkelijk getoond wordt. */
export function resolveTheme(m: ThemeMode = mode): 'light' | 'dark' {
  if (m === 'system') return mq.matches ? 'dark' : 'light'
  return m
}

function apply() {
  document.documentElement.setAttribute('data-theme', resolveTheme())
}

export function getThemeMode(): ThemeMode {
  return mode
}

export function setThemeMode(next: ThemeMode) {
  mode = next
  try {
    localStorage.setItem(KEY, next)
  } catch {
    // Niet kunnen opslaan is niet fataal — het thema geldt dan alleen deze sessie.
  }
  apply()
  listeners.forEach(l => l())
}

export function subscribeTheme(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** Eenmalig aanroepen bij het opstarten, vóór de eerste render. */
export function initTheme() {
  apply()
  // Ook in 'light'/'dark' blijven luisteren: terugschakelen naar 'system'
  // moet meteen het actuele OS-thema oppakken.
  mq.addEventListener('change', () => {
    if (mode === 'system') {
      apply()
      listeners.forEach(l => l())
    }
  })
}
