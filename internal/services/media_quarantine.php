<?php
/**
 * RDM media quarantaine — verplaatst bestanden uit wp-content/uploads naar een
 * quarantainemap buiten de webroot, of zet ze terug. Verwijdert nooit iets.
 *
 * Buiten de webroot is een bewuste keuze: de bestanden worden onbereikbaar, dus
 * gebroken links vallen op — en dat is precies de bedoeling. Ze staan tegelijk
 * buiten uploads, zodat een volgende scan ze niet als zwerfbestand terugvindt.
 *
 * De attachment-records in de database blijven staan. Daardoor is terugzetten een
 * kwestie van bestanden verplaatsen en klopt daarna alles weer, zonder database-
 * herstel.
 *
 * Alle staat zit in de klasse: `wp eval-file` includeert dit bestand binnen een
 * functie, waardoor top-level variabelen lokaal zijn.
 *
 * Uitvoer tussen sentinels, net als bij de scan.
 */

if (!defined('ABSPATH')) {
    fwrite(STDERR, "RDM: WordPress niet geladen\n");
    exit(1);
}

if (!function_exists('rdm_q_emit')) {
    function rdm_q_emit($payload)
    {
        $json = json_encode($payload, JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
        echo "\n<<<RDM-QUARANTINE-1>>>\n";
        echo base64_encode(gzencode($json, 6));
        echo "\n<<<END-RDM-QUARANTINE-1>>>\n";
    }
}

if (!class_exists('RdmMediaQuarantine')) {

final class RdmMediaQuarantine
{
    const MANIFEST = 'manifest.json';

    private $uploads;
    private $base;
    private $verplaatst = [];
    private $overgeslagen = [];
    private $bytes = 0;

    public function __construct($uploadsDir, $quarantaineBase)
    {
        $this->uploads = rtrim($uploadsDir, '/');
        $this->base    = rtrim($quarantaineBase, '/');
    }

    /**
     * veiligPad maakt van een relatief pad een absoluut pad binnen uploads, of geeft
     * "" terug. Elk pad komt van buiten, dus traversal (../) en absolute paden
     * worden geweigerd voordat er iets verplaatst wordt.
     */
    private function veiligPad($rel)
    {
        $rel = str_replace('\\', '/', (string) $rel);
        if ($rel === '' || $rel[0] === '/' || strpos($rel, '..') !== false || strpos($rel, "\0") !== false) {
            return '';
        }
        $vol  = $this->uploads . '/' . $rel;
        $echt = realpath($vol);
        if ($echt === false) {
            return '';
        }
        // Ook na het volgen van symlinks moet het binnen uploads blijven.
        $wortel = realpath($this->uploads);
        if ($wortel === false || strpos($echt, $wortel . '/') !== 0) {
            return '';
        }
        return $echt;
    }

    /**
     * variantenVan zoekt de door WordPress gegenereerde formaten van een bestand:
     * thumbnails, -scaled, webp-varianten en editor-backups in dezelfde map. Alleen
     * het origineel verplaatsen laat de thumbnails staan, en dan levert het niets op.
     */
    private function variantenVan($absPad)
    {
        $map  = dirname($absPad);
        $naam = basename($absPad);
        $punt = strrpos($naam, '.');
        if ($punt === false) {
            return [];
        }
        $stam = substr($naam, 0, $punt);
        $ext  = substr($naam, $punt + 1);

        $patroon = $map . '/' . $this->escapeGlob($stam) . '*';
        $uit     = [];
        foreach ((array) glob($patroon) as $kandidaat) {
            if (!is_file($kandidaat) || $kandidaat === $absPad) {
                continue;
            }
            $kNaam = basename($kandidaat);
            $regex = '/^' . preg_quote($stam, '/')
                . '(-\d+x\d+|-scaled|@2x|-e\d{9,})*'
                . '(\.' . preg_quote($ext, '/') . '|\.webp|\.avif|\.' . preg_quote($ext, '/') . '\.webp)$/i';
            if (preg_match($regex, $kNaam)) {
                $uit[] = $kandidaat;
            }
        }
        return $uit;
    }

    private function escapeGlob($s)
    {
        return str_replace(['*', '?', '[', ']'], ['\\*', '\\?', '\\[', '\\]'], $s);
    }

    /** Verplaats één bestand en houd bij wat er gebeurde. */
    private function verplaats($absVan, $naarBasis, $batch)
    {
        $rel  = substr($absVan, strlen(realpath($this->uploads)) + 1);
        $doel = $naarBasis . '/' . $rel;
        $map  = dirname($doel);
        if (!is_dir($map) && !mkdir($map, 0755, true) && !is_dir($map)) {
            $this->overgeslagen[] = ['path' => $rel, 'reason' => 'kon de quarantainemap niet aanmaken'];
            return;
        }
        if (file_exists($doel)) {
            $this->overgeslagen[] = ['path' => $rel, 'reason' => 'staat al in quarantaine'];
            return;
        }
        $grootte = (int) @filesize($absVan);
        if (!@rename($absVan, $doel)) {
            $this->overgeslagen[] = ['path' => $rel, 'reason' => 'verplaatsen mislukte (rechten?)'];
            return;
        }
        $this->bytes += $grootte;
        $this->verplaatst[] = ['original' => $rel, 'stored' => $batch . '/' . $rel, 'bytes' => $grootte];
    }

    /** Verplaats de opgegeven bestanden plus hun formaten naar een nieuwe batch. */
    public function quarantaine($batch, array $paden)
    {
        $batchMap = $this->base . '/' . $batch;
        if (!is_dir($batchMap) && !mkdir($batchMap, 0755, true) && !is_dir($batchMap)) {
            return ['error' => 'kon de quarantainemap niet aanmaken: ' . $batchMap];
        }

        foreach ($paden as $rel) {
            $abs = $this->veiligPad($rel);
            if ($abs === '') {
                $this->overgeslagen[] = ['path' => (string) $rel, 'reason' => 'pad bestaat niet of valt buiten uploads'];
                continue;
            }
            foreach ($this->variantenVan($abs) as $variant) {
                $this->verplaats($variant, $batchMap, $batch);
            }
            $this->verplaats($abs, $batchMap, $batch);
        }

        $manifest = [
            'batch'   => $batch,
            'uploads' => realpath($this->uploads),
            'created' => gmdate('c'),
            'entries' => $this->verplaatst,
            'bytes'   => $this->bytes,
        ];
        file_put_contents($batchMap . '/' . self::MANIFEST, json_encode($manifest, JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT));

        return $this->resultaat($batch);
    }

    /** Zet een batch terug op zijn oorspronkelijke plek. */
    public function herstel($batch)
    {
        $batchMap = $this->base . '/' . $batch;
        $mPad     = $batchMap . '/' . self::MANIFEST;
        if (!is_file($mPad)) {
            return ['error' => 'geen manifest voor batch ' . $batch];
        }
        $manifest = json_decode((string) file_get_contents($mPad), true);
        if (!is_array($manifest) || empty($manifest['entries'])) {
            return ['error' => 'manifest van batch ' . $batch . ' is onleesbaar'];
        }

        $rest = [];
        foreach ($manifest['entries'] as $entry) {
            $rel = isset($entry['original']) ? (string) $entry['original'] : '';
            if ($rel === '' || strpos($rel, '..') !== false || $rel[0] === '/') {
                $this->overgeslagen[] = ['path' => $rel, 'reason' => 'ongeldig pad in manifest'];
                continue;
            }
            $van   = $batchMap . '/' . $rel;
            $terug = $this->uploads . '/' . $rel;
            if (!is_file($van)) {
                $this->overgeslagen[] = ['path' => $rel, 'reason' => 'staat niet meer in quarantaine'];
                continue;
            }
            if (file_exists($terug)) {
                // Nooit overschrijven: er kan inmiddels een nieuw bestand staan.
                $this->overgeslagen[] = ['path' => $rel, 'reason' => 'er staat weer een bestand op deze plek'];
                $rest[]               = $entry;
                continue;
            }
            $map = dirname($terug);
            if (!is_dir($map) && !mkdir($map, 0755, true) && !is_dir($map)) {
                $this->overgeslagen[] = ['path' => $rel, 'reason' => 'kon de oorspronkelijke map niet aanmaken'];
                $rest[]               = $entry;
                continue;
            }
            if (!@rename($van, $terug)) {
                $this->overgeslagen[] = ['path' => $rel, 'reason' => 'terugzetten mislukte (rechten?)'];
                $rest[]               = $entry;
                continue;
            }
            $this->bytes += isset($entry['bytes']) ? (int) $entry['bytes'] : 0;
            $this->verplaatst[] = ['original' => $rel, 'stored' => '', 'bytes' => isset($entry['bytes']) ? (int) $entry['bytes'] : 0];
        }

        // Wat terug is, uit het manifest halen; de rest blijft staan met reden.
        $manifest['entries'] = $rest;
        $manifest['bytes']   = 0;
        foreach ($rest as $e) {
            $manifest['bytes'] += isset($e['bytes']) ? (int) $e['bytes'] : 0;
        }
        file_put_contents($mPad, json_encode($manifest, JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT));

        return $this->resultaat($batch);
    }

    /** Overzicht van alle batches die in quarantaine staan. */
    public function lijst()
    {
        $batches = [];
        foreach ((array) glob($this->base . '/*/' . self::MANIFEST) as $mPad) {
            $m = json_decode((string) file_get_contents($mPad), true);
            if (!is_array($m)) {
                continue;
            }
            $batches[] = [
                'batch'   => isset($m['batch']) ? (string) $m['batch'] : basename(dirname($mPad)),
                'created' => isset($m['created']) ? (string) $m['created'] : '',
                'files'   => isset($m['entries']) ? count($m['entries']) : 0,
                'bytes'   => isset($m['bytes']) ? (int) $m['bytes'] : 0,
            ];
        }
        usort($batches, function ($a, $b) {
            return strcmp($b['created'], $a['created']);
        });
        return ['action' => 'list', 'batches' => $batches, 'quarantineDir' => $this->base];
    }

    private function resultaat($batch)
    {
        return [
            'batch'         => $batch,
            'moved'         => $this->verplaatst,
            'skipped'       => $this->overgeslagen,
            'bytes'         => $this->bytes,
            'quarantineDir' => $this->base,
        ];
    }
}

} // class_exists

$rdmQIn = getenv('RDM_Q_INPUT');
$rdmQ   = $rdmQIn !== false && $rdmQIn !== '' ? json_decode((string) base64_decode($rdmQIn, true), true) : null;
if (!is_array($rdmQ) || empty($rdmQ['action'])) {
    rdm_q_emit(['error' => 'geen geldige opdracht meegegeven']);
    return;
}

$rdmUpdir   = wp_get_upload_dir();
$rdmBaseEnv = getenv('RDM_Q_BASE');
if ($rdmBaseEnv !== false && $rdmBaseEnv !== '') {
    $rdmBase = (string) $rdmBaseEnv;
} elseif (isset($rdmQ['base']) && $rdmQ['base'] !== '') {
    $rdmBase = (string) $rdmQ['base'];
} else {
    // Naast de webroot, niet erin: bestanden moeten onbereikbaar worden.
    $rdmBase = dirname(rtrim(ABSPATH, '/')) . '/rdm-quarantine';
}
$rdmQuar  = new RdmMediaQuarantine($rdmUpdir['basedir'], $rdmBase);

switch ($rdmQ['action']) {
    case 'quarantine':
        rdm_q_emit($rdmQuar->quarantaine((string) $rdmQ['batch'], isset($rdmQ['paths']) ? (array) $rdmQ['paths'] : []));
        break;
    case 'restore':
        rdm_q_emit($rdmQuar->herstel((string) $rdmQ['batch']));
        break;
    case 'list':
        rdm_q_emit($rdmQuar->lijst());
        break;
    default:
        rdm_q_emit(['error' => 'onbekende opdracht: ' . $rdmQ['action']]);
}
