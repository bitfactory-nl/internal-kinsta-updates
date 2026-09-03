#!/usr/bin/env bash
# Bouwt de "## Wijzigingen"-sectie voor de release-notes uit de commits sinds de
# vorige v-tag. De app parseert precies deze vorm (zie
# internal/services/update_notes.go), dus de koppen moeten letterlijk
# "### Nieuw", "### Opgelost" en "### Overig" heten.
#
# Gebruik: .github/scripts/changelog.sh v0.2.10
set -euo pipefail

TAG="${1:?geef de tag mee, bijvoorbeeld v0.2.10}"

# --match voorkomt dat de bewegende tag kinsta-latest als "vorige versie" wordt
# gekozen; die staat immers ook in de tag-lijst.
PREV="$(git describe --tags --abbrev=0 --match 'v*.*.*' "${TAG}^" 2>/dev/null || true)"
if [ -n "$PREV" ]; then
  RANGE="${PREV}..${TAG}"
else
  RANGE="$TAG"
fi

SUBJECTS="$(git log --no-merges --pretty=format:%s "$RANGE")"

nieuw=""
opgelost=""
overig=""

while IFS= read -r subject; do
  [ -n "$subject" ] || continue
  case "$subject" in
    feat:*|feat\(*\):*)
      nieuw="${nieuw}- ${subject#*: }"$'\n' ;;
    fix:*|fix\(*\):*)
      opgelost="${opgelost}- ${subject#*: }"$'\n' ;;
    *)
      # Overige commits met een conventional-commit prefix laten we de prefix
      # houden: "chore: deps bijwerken" is zonder dat woord minder duidelijk.
      overig="${overig}- ${subject}"$'\n' ;;
  esac
done <<< "$SUBJECTS"

if [ -z "$nieuw" ] && [ -z "$opgelost" ] && [ -z "$overig" ]; then
  exit 0
fi

echo "## Wijzigingen"
echo
if [ -n "$nieuw" ]; then
  echo "### Nieuw"
  printf '%s' "$nieuw"
  echo
fi
if [ -n "$opgelost" ]; then
  echo "### Opgelost"
  printf '%s' "$opgelost"
  echo
fi
if [ -n "$overig" ]; then
  echo "### Overig"
  printf '%s' "$overig"
  echo
fi
