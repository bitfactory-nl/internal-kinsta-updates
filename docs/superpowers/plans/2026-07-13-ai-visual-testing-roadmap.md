# AI visuele + functionele test-engine — Plan-roadmap (Feature A)

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md)

Deze feature is opgesplitst in vijf opeenvolgende deelplannen. Elk plan levert
werkende, testbare software op en bouwt voort op het vorige.

| # | Plan | Levert op | Status |
|---|------|-----------|--------|
| 1 | **Fundamenten** (Go, pure logic) | Domain-types, config-uitbreiding, flow-file lezen/valideren, regressie-diff, model-routing — allemaal met unit-tests | ✅ gebouwd + geverifieerd |
| 2 | **Playwright-sidecar + adapters/browser** | Node-runner + Go-adapter + JSON-protocol; speelt een flow af, levert screenshots/console/netwerk; contracttest | ✅ gebouwd + geverifieerd |
| 3 | **adapters/claude (Anthropic)** | API-client met test-seam: authoring (NL→stappen), visuele vergelijking, self-heal; model-routing aangesloten | ✅ gebouwd + geverifieerd |
| 4 | **test_service + orchestratie + Wails** | Run-levenscyclus, historie-opslag, Wails-bindings; end-to-end aanroepbaar vanuit de frontend | ✅ gebouwd + geverifieerd |
| 5 | **TestsTab (frontend)** | UI in de huisstijl: flows, run-config, resultaten, historie | ✅ gebouwd + geverifieerd |

**Volgorde is bindend:** 2 hangt af van 1 (types), 3 van 1, 4 van 2+3, 5 van 4.

Na akkoord op Plan 1 en uitvoering ervan schrijf ik Plan 2, enzovoort — zo blijft elk
plan actueel t.o.v. de daadwerkelijk gebouwde code.
