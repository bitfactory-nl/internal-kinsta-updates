# internal-kinsta-updates

Centrale reusable GitHub Actions workflow voor het automatisch checken van WordPress- en NPM-updates op Kinsta-hosted projecten.

## Wat doet het?

Elke maandag om 08:00 (of handmatig via `workflow_dispatch`) wordt per project:

1. **WordPress-updates** opgehaald via WP-CLI over SSH (core, plugins, themes)
2. **NPM-updates** (minor + patch) gecontroleerd via `npm-check-updates`
3. Een `automated/updates-{datum}` branch + PR aangemaakt als er updates zijn
4. De vorige update-branch en bijbehorende PR automatisch gesloten

## Gebruik in een project

Voeg dit bestand toe als `.github/workflows/check-updates.yml` in het project:

```yaml
name: Check Updates

on:
  schedule:
    - cron: '0 8 * * 1'
  workflow_dispatch:

jobs:
  check-updates:
    uses: bitfactory-nl/internal-kinsta-updates/.github/workflows/check-updates.yml@main
    secrets: inherit
```

## Benodigde secrets (per project repo)

| Secret              | Omschrijving                          |
|---------------------|---------------------------------------|
| `KINSTA_SERVER_IP`  | SSH host van de Kinsta-omgeving       |
| `KINSTA_USERNAME`   | SSH gebruikersnaam                    |
| `PASSWORD`          | SSH wachtwoord                        |
| `PORT`              | SSH poort                             |
| `KINSTA_SITE_PATH`  | Absoluut pad naar de WordPress-root   |

## Branch-naamgeving

De gegenereerde branches volgen het patroon `automated/updates-YYYY-MM-DDTHH-MM-SS`. De RDM Sites Tool herkent deze automatisch in de Updates-tab.
