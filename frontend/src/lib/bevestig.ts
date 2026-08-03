import { Dialogs } from '@wailsio/runtime'

// bevestig toont een native ja/nee-dialoog via de Wails-runtime.
//
// window.confirm() is in deze app onbruikbaar: de WKWebView implementeert het
// confirm-paneel niet, dus het geeft stil `false` terug en elke knop die erop
// wacht "doet niks". Gebruik daarom altijd deze helper voor bevestigingen.
export async function bevestig(titel: string, bericht: string): Promise<boolean> {
  try {
    const keuze = await Dialogs.Question({
      Title: titel,
      Message: bericht,
      Buttons: [
        { Label: 'Annuleer', IsCancel: true },
        { Label: 'Doorgaan', IsDefault: true },
      ],
    })
    return keuze === 'Doorgaan'
  } catch {
    // Geen dialoog kunnen tonen betekent geen toestemming — nooit stil doorgaan.
    return false
  }
}
