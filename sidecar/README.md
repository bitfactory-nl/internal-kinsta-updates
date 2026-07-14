# Playwright sidecar

Speelt een happy flow af op twee omgevingen en levert per stap screenshot,
console-errors en HTTP-statuscodes. Aangestuurd door het Go-pakket
`internal/adapters/browser` via JSON over stdin/stdout.

## Installatie (eenmalig, lokaal / in de app-build)
    cd sidecar
    npm install
    npm run install-browser   # downloadt Chromium

## Protocol
- stdin: één JSON-object (RunRequest, zie internal/adapters/browser/protocol.go).
- stdout: één JSON-object (RunResponse).
- Fouten in één stap komen in steps[].error (+ steps[].snapshot met de
  accessibility-tree voor self-heal); een fatale fout komt in top-level error.

## Smoke-test (handmatig; vereist Chromium)
Serveer de fixtures en draai een request. Zie testdata/. Deze test is niet in
CI opgenomen omdat Chromium een grote download is.
