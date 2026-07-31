<?php
/**
 * RDM media scan — READ-ONLY analyse van wp-content/uploads.
 *
 * Draait via `wp eval-file` op de server, zodat de zware kant (honderdduizenden
 * bestanden, honderden MB content) bij de data blijft en er één compacte JSON
 * over de SSH-verbinding gaat.
 *
 * Alle staat zit in de klasse, niet in scriptvariabelen: `wp eval-file` includeert
 * dit bestand BINNEN een functie, waardoor top-level variabelen lokaal zijn en
 * `global $x` in een helper niets oplevert. Alleen `$wpdb` is een echte WordPress-
 * global en mag zo worden opgehaald.
 *
 * Dit script schrijft nooit: geen unlink, geen rename, geen UPDATE/DELETE/INSERT.
 * Een test in Go dwingt dat af (media_scan_php_test.go).
 *
 * Uitvoer staat tussen sentinels omdat WP-CLI en plugins ongevraagd notices op
 * stdout printen:
 *
 *   <<<RDM-MEDIA-1>>>
 *   base64(gzip(json))
 *   <<<END-RDM-MEDIA-1>>>
 */

if (!defined('ABSPATH')) {
    fwrite(STDERR, "RDM: WordPress niet geladen\n");
    exit(1);
}

/** rdm_emit is een functie en dus altijd bereikbaar, ongeacht de include-scope. */
if (!function_exists('rdm_emit')) {
    function rdm_emit($payload)
    {
        $json = json_encode($payload, JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
        echo "\n<<<RDM-MEDIA-1>>>\n";
        echo base64_encode(gzencode($json, 6));
        echo "\n<<<END-RDM-MEDIA-1>>>\n";
    }
}

if (!class_exists('RdmMediaScan')) {

final class RdmMediaScan
{
    /** Batchgrootte voor queries; altijd op de primary key, nooit met OFFSET. */
    const BATCH = 2000;
    /** Kleinere batch voor content: één pagina vol pagebuilder-data kan megabytes zijn. */
    const BATCH_CONTENT = 200;
    /** Aantal grootste bestanden dat wordt bijgehouden. */
    const TOP = 100;
    /** Voorbeeldrijen per categorie in de samenvatting. */
    const SAMPLE = 500;
    /** Maximum detailrijen per categorie. */
    const DETAIL_CAP = 20000;

    private $db;
    private $start;
    private $budget;

    private $basedir = '';
    private $baseurl = '';
    /** Gekozen mappen (relatief aan uploads); leeg = de hele boom. '.' = de hoofdmap. */
    private $folders = [];

    private $byId = [];      // id => ['file','title','mime','date']
    private $pathToId = [];   // sleutel(relpad) => id
    private $baseToId = [];   // sleutel(basename) => id
    private $onDisk = [];     // sleutel(relpad) => bytes
    private $refs = [];       // id => ['content' => true, ...]

    private $totalFiles = 0;
    private $totalBytes = 0;
    private $byClass = [];
    private $byPeriod = [];
    private $largest = [];

    private $orphans = [];
    private $orphanCount = 0;
    private $orphanBytes = 0;
    private $missing = [];
    private $missingCount = 0;
    private $missingIds = [];
    private $unref = [];
    private $unrefCount = 0;
    private $unrefBytes = 0;
    private $inUse = [];
    private $inUseBytes = 0;
    private $refCount = 0;

    private $tables = [];
    /** Aantal doorzochte rijen per bron: maakt zichtbaar of de referentiescan écht
     *  door de content is gegaan, in plaats van dat je dat uit een uitkomst moet raden. */
    private $rowsScanned = [];
    private $themeFiles = 0;
    private $notes = [];
    private $offload = false;

    // Elke fase weet zelf of hij compleet is. Een afgekapte fase maakt de conclusies
    // die eruit volgen ongeldig, en dan hoort er geen getal te staan.
    private $indexComplete = true;
    private $walkComplete = true;
    private $refScanComplete = false;

    public function __construct($db, $budget, array $folders = [])
    {
        $this->db      = $db;
        $this->budget  = (float) $budget;
        $this->folders = array_values(array_filter(array_map('trim', $folders), 'strlen'));
        $this->start   = microtime(true);
    }

    public function run()
    {
        $updir = wp_get_upload_dir();
        $this->basedir = rtrim($updir['basedir'], '/');
        $this->baseurl = $updir['baseurl'];

        if (!is_dir($this->basedir)) {
            return ['error' => 'uploads-map bestaat niet: ' . $this->basedir];
        }

        $this->hartslag('bibliotheek indexeren');
        $this->indexAttachments();
        $this->indexGeneratedSizes();
        $this->walkFiles();
        $this->findMissing();
        $this->hartslag('referenties zoeken');
        $this->collectReferences();
        $this->findUnreferenced();
        $this->hartslag('klaar');

        return $this->payload();
    }

    // --- hulpmiddelen ---

    private function overBudget()
    {
        return (microtime(true) - $this->start) > $this->budget;
    }

    /**
     * hartslag print een voortgangsregel. Die valt buiten de sentinels en wordt dus
     * door de parser genegeerd, maar houdt de SSH-verbinding bezig: een scan die
     * minuten lang niets stuurt, wordt door tussenliggende firewalls afgekapt.
     */
    private function hartslag($fase)
    {
        printf("#RDM-PHASE %s t=%ds bestanden=%d\n", $fase, (int) (microtime(true) - $this->start), $this->totalFiles);
        flush();
    }

    /** 64-bits sleutel: int-keys houden een index van 300k paden rond de 10 MB. */
    private static function sleutel($s)
    {
        $u = unpack('J', substr(md5($s, true), 0, 8));
        return $u[1];
    }

    /** Formaatsuffixen strippen: -300x200, -scaled, @2x, -e1712345678. */
    private static function zonderFormaat($name)
    {
        $name = preg_replace('/-e\d{9,}(?=\.[A-Za-z0-9]+$)/', '', $name);
        $name = preg_replace('/-\d+x\d+(?=\.[A-Za-z0-9]+$)/', '', $name);
        $name = preg_replace('/-scaled(?=\.[A-Za-z0-9]+$)/', '', $name);
        $name = preg_replace('/@2x(?=\.[A-Za-z0-9]+$)/', '', $name);
        return $name;
    }

    /** Mappen en extensies die plugins gebruiken voor caches, exports en backups. */
    private static function isSysteemPad($rel)
    {
        static $delen = [
            'cache/', 'wp-rocket/', 'wpo-cache/', 'litespeed/', 'elementor/css/', 'et-cache/',
            'wpforms/', 'gravity_forms/', 'wpcf7_uploads/', 'formidable/', 'ninja-forms/',
            'wp-personal-data-exports/', 'backwpup', 'updraft', 'wp-migrate-db/', 'wpvivid',
            'backup', 'woocommerce_uploads/', 'wc-logs/', 'shortpixel-backups/', 'ewww/',
            'smush-webp/', 'sitemaps/', 'wp-statistics/',
        ];
        $l = strtolower($rel);
        foreach ($delen as $d) {
            if (strpos($l, $d) !== false) {
                return true;
            }
        }
        return preg_match('/\.(zip|gz|tar|sql|log|bak|tmp|txt|json|xml|css|js)$/i', $l) === 1;
    }

    /**
     * periodeVan geeft het padvoorvoegsel waar een bestand onder valt: "2024/05"
     * voor de standaard WordPress-indeling, anders de bovenste map, en "." voor
     * bestanden die los in uploads staan. Dit is een echt pad, geen label, zodat
     * de UI het rechtstreeks als scan-selectie kan teruggeven.
     */
    private static function periodeVan($rel)
    {
        $delen = explode('/', $rel);
        if (count($delen) < 2) {
            return '.';
        }
        if (count($delen) >= 3 && preg_match('/^\d{4}$/', $delen[0]) && preg_match('/^\d{2}$/', $delen[1])) {
            return $delen[0] . '/' . $delen[1];
        }
        return $delen[0];
    }

    /** Klasse van een bestand: wat heeft het gemaakt? */
    private function klasseVan($rel, $bekend)
    {
        $name = basename($rel);
        if (self::isSysteemPad($rel)) {
            return 'system';
        }
        if (preg_match('/-e\d{9,}\.[A-Za-z0-9]+$/', $name)) {
            return 'editor_backup';
        }
        if (preg_match('/\.(webp|avif)$/i', $name)) {
            $gestript = preg_replace('/\.(webp|avif)$/i', '', $name);
            if (isset($this->baseToId[self::sleutel($gestript)]) || strpos($gestript, '.') !== false) {
                return 'nextgen';
            }
        }
        if (preg_match('/-scaled\.[A-Za-z0-9]+$/', $name)) {
            return 'scaled';
        }
        if (preg_match('/-\d+x\d+\.[A-Za-z0-9]+$/', $name)) {
            return 'generated';
        }
        return $bekend ? 'original' : 'unknown';
    }

    // --- 1. mediabibliotheek indexeren ---

    /**
     * mapFilter beperkt de index tot de gekozen mappen. Zo blijft een gerichte scan
     * ook aan de databasekant klein, en niet alleen bij het doorlopen van de schijf.
     */
    private function mapFilter()
    {
        if (!$this->folders) {
            return ['', []];
        }
        $delen = [];
        $args  = [];
        foreach ($this->folders as $f) {
            if ($f === '.') {
                // %% omdat de placeholder pas door prepare() heen gaat.
                $delen[] = "pm.meta_value NOT LIKE '%%/%%'";
                continue;
            }
            // Nooit twee keer prepare(): de LIKE-waarde bevat zelf een %, en dan
            // struikelt de buitenste prepare over zijn eigen placeholder.
            $delen[] = 'pm.meta_value LIKE %s';
            $args[]  = $this->db->esc_like($f . '/') . '%';
        }
        return [' AND (' . implode(' OR ', $delen) . ')', $args];
    }

    private function indexAttachments()
    {
        try {
            $this->indexAttachmentsBatches();
        } catch (Throwable $e) {
            $this->indexComplete = false;
            $this->notes[]       = 'indexeren afgebroken: ' . $e->getMessage();
        }
    }

    private function indexAttachmentsBatches()
    {
        list($filter, $filterArgs) = $this->mapFilter();
        $laatste = 0;
        while (true) {
            $args  = array_merge([$laatste], $filterArgs, [self::BATCH]);
            $rijen = $this->haalRijen($this->db->prepare(
                "SELECT p.ID, p.post_title, p.post_mime_type, p.post_date_gmt, pm.meta_value AS file
                   FROM {$this->db->posts} p
                   JOIN {$this->db->postmeta} pm ON pm.post_id = p.ID AND pm.meta_key = '_wp_attached_file'
                  WHERE p.post_type = 'attachment' AND p.ID > %d" . $filter . "
                  ORDER BY p.ID ASC LIMIT %d",
                ...$args
            ), 'attachments');
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                $id      = (int) $r['ID'];
                $laatste = $id;
                $rel     = ltrim((string) $r['file'], '/');
                if ($rel === '') {
                    continue;
                }
                $this->byId[$id] = [
                    'file'  => $rel,
                    'title' => (string) $r['post_title'],
                    'mime'  => (string) $r['post_mime_type'],
                    'date'  => (int) strtotime((string) $r['post_date_gmt'] . ' UTC'),
                ];
                $this->pathToId[self::sleutel($rel)]             = $id;
                $this->baseToId[self::sleutel(basename($rel))]   = $id;
            }
            if ($this->overBudget()) {
                $this->indexComplete = false;
                $this->notes[]       = 'tijdsbudget geraakt tijdens het indexeren van de mediabibliotheek';
                return;
            }
        }
    }

    private function indexGeneratedSizes()
    {
        if (!$this->indexComplete) {
            return;
        }
        try {
            $this->indexGeneratedSizesBatches();
        } catch (Throwable $e) {
            $this->indexComplete = false;
            $this->notes[]       = 'indexeren van formaten afgebroken: ' . $e->getMessage();
        }
    }

    private function indexGeneratedSizesBatches()
    {
        $laatste = 0;
        while (true) {
            $rijen = $this->haalRijen($this->db->prepare(
                "SELECT meta_id, post_id, meta_value
                   FROM {$this->db->postmeta}
                  WHERE meta_key IN ('_wp_attachment_metadata','_wp_attachment_backup_sizes')
                    AND meta_id > %d
                  ORDER BY meta_id ASC LIMIT %d",
                $laatste,
                self::BATCH
            ), 'attachment_sizes');
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                $laatste = (int) $r['meta_id'];
                $id      = (int) $r['post_id'];
                if (!isset($this->byId[$id])) {
                    continue;
                }
                $map = trim(dirname($this->byId[$id]['file']), '.');
                $map = $map === '' ? '' : rtrim($map, '/') . '/';
                $waarde = (string) $r['meta_value'];
                if (preg_match_all('/"file";s:\d+:"([^"]+)"/', $waarde, $m)) {
                    foreach ($m[1] as $f) {
                        $this->pathToId[self::sleutel($map . $f)]           = $id;
                        $this->baseToId[self::sleutel(basename($f))]        = $id;
                    }
                }
                if (preg_match('/"original_image";s:\d+:"([^"]+)"/', $waarde, $mo)) {
                    $this->pathToId[self::sleutel($map . $mo[1])]           = $id;
                    $this->baseToId[self::sleutel(basename($mo[1]))]        = $id;
                }
            }
            if ($this->overBudget()) {
                $this->indexComplete = false;
                $this->notes[]       = 'tijdsbudget geraakt tijdens het indexeren van afbeeldingsformaten';
                return;
            }
        }
    }

    // --- 2. bestanden op schijf ---

    private function walkFiles()
    {
        if (!$this->indexComplete) {
            $this->notes[] = 'zwerfbestanden niet bepaald: de mediabibliotheek is niet volledig geïndexeerd';
        }
        $this->hartslag('bestanden doorlopen');
        $prefix = strlen($this->basedir) + 1;

        foreach ($this->doorloopIterators() as $it) {
        foreach ($it as $bestand) {
            if (!$bestand->isFile()) {
                continue;
            }
            $rel = substr($bestand->getPathname(), $prefix);
            if ($rel === false || $rel === '') {
                continue;
            }
            $bytes = (int) $bestand->getSize();
            $mtime = (int) $bestand->getMTime();

            $this->totalFiles++;
            $this->totalBytes += $bytes;
            $this->onDisk[self::sleutel($rel)] = $bytes;

            $id     = isset($this->pathToId[self::sleutel($rel)]) ? $this->pathToId[self::sleutel($rel)] : 0;
            $klasse = $this->klasseVan($rel, $id > 0);

            if (!isset($this->byClass[$klasse])) {
                $this->byClass[$klasse] = ['files' => 0, 'bytes' => 0];
            }
            $this->byClass[$klasse]['files']++;
            $this->byClass[$klasse]['bytes'] += $bytes;

            $periode = self::periodeVan($rel);
            if (!isset($this->byPeriod[$periode])) {
                $this->byPeriod[$periode] = ['files' => 0, 'bytes' => 0];
            }
            $this->byPeriod[$periode]['files']++;
            $this->byPeriod[$periode]['bytes'] += $bytes;

            $rij = ['path' => $rel, 'bytes' => $bytes, 'modifiedAt' => $mtime, 'class' => $klasse, 'attachmentId' => $id];
            if (count($this->largest) < self::TOP) {
                $this->largest[] = $rij;
                if (count($this->largest) === self::TOP) {
                    $this->sorteerGrootste();
                }
            } elseif ($bytes > $this->largest[count($this->largest) - 1]['bytes']) {
                $this->largest[count($this->largest) - 1] = $rij;
                $this->sorteerGrootste();
            }

            // Categorie A: niet in de bibliotheek, en ook geen afgeleide van een
            // bekend bestand of bekende plugin-rommel. Die tellen wél mee in de
            // groottes, maar horen niet in een lijst die "staat niet in de
            // mediabibliotheek" heet. Zonder volledige index is deze uitspraak
            // waardeloos: dan kan een bestand er wél in staan zonder dat we het
            // hebben gezien.
            if ($this->indexComplete
                && $id === 0 && !in_array($klasse, ['generated', 'nextgen', 'editor_backup', 'scaled', 'system'], true)
                && !isset($this->baseToId[self::sleutel(self::zonderFormaat(basename($rel)))])) {
                $this->orphanCount++;
                $this->orphanBytes += $bytes;
                if (count($this->orphans) < self::DETAIL_CAP) {
                    $this->orphans[] = [
                        'path' => $rel, 'bytes' => $bytes, 'modifiedAt' => $mtime,
                        'class' => $klasse, 'category' => 'orphan_file',
                    ];
                }
            }

            if (($this->totalFiles % 20000) === 0) {
                $this->hartslag('bestanden doorlopen');
            }
            if (($this->totalFiles % 5000) === 0 && $this->overBudget()) {
                $this->walkComplete = false;
                $this->notes[]      = 'tijdsbudget geraakt tijdens het doorlopen van de bestanden';
                break 2;
            }
        }
        }
        $this->sorteerGrootste();
    }

    /**
     * doorloopIterators levert de te doorlopen mappen: de hele boom, of alleen de
     * gekozen mappen. Dat laatste is de reden dat een gerichte scan snel is — de
     * bestandsdoorloop is het duurste onderdeel op netwerkopslag.
     */
    private function doorloopIterators()
    {
        if (!$this->folders) {
            return [new RecursiveIteratorIterator(
                new RecursiveDirectoryIterator($this->basedir, FilesystemIterator::SKIP_DOTS),
                RecursiveIteratorIterator::LEAVES_ONLY,
                RecursiveIteratorIterator::CATCH_GET_CHILD
            )];
        }

        $uit = [];
        foreach ($this->folders as $f) {
            if ($f === '.') {
                // Alleen de losse bestanden in de hoofdmap, niet de submappen.
                $uit[] = new FilesystemIterator($this->basedir, FilesystemIterator::SKIP_DOTS);
                continue;
            }
            $pad = $this->basedir . '/' . ltrim($f, '/');
            if (is_dir($pad)) {
                $uit[] = new RecursiveIteratorIterator(
                    new RecursiveDirectoryIterator($pad, FilesystemIterator::SKIP_DOTS),
                    RecursiveIteratorIterator::LEAVES_ONLY,
                    RecursiveIteratorIterator::CATCH_GET_CHILD
                );
            } else {
                $this->notes[] = 'map niet gevonden op de server: ' . $f;
            }
        }
        return $uit;
    }

    private function sorteerGrootste()
    {
        usort($this->largest, function ($a, $b) {
            return $b['bytes'] <=> $a['bytes'];
        });
    }

    // --- 3. bibliotheek-items zonder bestand ---

    private function findMissing()
    {
        // Alleen betrouwbaar als álle bestanden én de hele bibliotheek gezien zijn:
        // anders lijkt elk niet-bekeken bestand "weg".
        if (!$this->indexComplete || !$this->walkComplete) {
            $this->notes[] = 'ontbrekende bestanden niet bepaald: de scan zag niet alles';
            return;
        }
        foreach ($this->byId as $id => $a) {
            if (isset($this->onDisk[self::sleutel($a['file'])])) {
                continue;
            }
            $this->missingCount++;
            $this->missingIds[$id] = true;
            if (count($this->missing) < self::DETAIL_CAP) {
                $this->missing[] = [
                    'path' => $a['file'], 'bytes' => 0, 'modifiedAt' => $a['date'],
                    'class' => 'unknown', 'category' => 'missing_file',
                    'attachmentId' => $id, 'title' => $a['title'], 'mimeType' => $a['mime'],
                ];
            }
        }

        $this->offload = (int) $this->db->get_var(
            "SELECT COUNT(*) FROM {$this->db->options}
              WHERE option_name IN ('tantan_wordpress_s3','as3cf_settings','wpos3_settings')"
        ) > 0;
        if ($this->offload) {
            $this->notes[] = 'offload-plugin gevonden: ontbrekende bestanden staan waarschijnlijk in externe opslag';
        }
    }

    // --- 4. referenties ---

    private function tel($bron, $aantal)
    {
        if (!isset($this->rowsScanned[$bron])) {
            $this->rowsScanned[$bron] = 0;
        }
        $this->rowsScanned[$bron] += $aantal;
    }

    /**
     * haalRijen voert één batch uit en telt hem. Cruciaal: $wpdb->get_results()
     * geeft bij een SQL-fout óók een lege array terug. Zonder deze controle leest
     * de lus dat als "klaar" en meldt de scan "geen referenties gevonden" terwijl er
     * in werkelijkheid niets is doorzocht — een stille nul is het gevaarlijkste
     * antwoord dat dit script kan geven.
     */
    private function haalRijen($sql, $bron)
    {
        $rijen = $this->db->get_results($sql, ARRAY_A);
        if (!empty($this->db->last_error)) {
            throw new RuntimeException('databasefout bij ' . $bron . ': ' . $this->db->last_error);
        }
        $this->tel($bron, is_array($rijen) ? count($rijen) : 0);
        return $rijen;
    }

    /** Zet het bewijs-bit voor elk attachment waar deze tekst naar verwijst. */
    private function collect($tekst, $bron)
    {
        if (!is_string($tekst) || $tekst === '') {
            return;
        }

        if (preg_match_all('/(?:wp-image-|wp-att-|attachment[_-]|"id"\s*:\s*|attachment_id"?[:=]\s*"?)(\d+)/i', $tekst, $m)) {
            foreach ($m[1] as $ruw) {
                $id = (int) $ruw;
                if (isset($this->byId[$id])) {
                    $this->refs[$id][$bron] = true;
                }
            }
        }
        if (preg_match_all('/\[gallery[^\]]*ids="([\d,\s]+)"/i', $tekst, $mg)) {
            foreach ($mg[1] as $lijst) {
                foreach (preg_split('/[,\s]+/', $lijst) as $ruw) {
                    $id = (int) $ruw;
                    if ($id > 0 && isset($this->byId[$id])) {
                        $this->refs[$id][$bron] = true;
                    }
                }
            }
        }

        // Twee patronen: een pad achter "uploads/" (gewone URL's) én een los
        // jaar/maand-pad zoals plugins dat bewaren ("2020/04/foto.jpg"). Zonder dat
        // tweede patroon mist elke plugin die relatieve paden opslaat.
        $paden = [];
        if (strpos($tekst, 'uploads/') !== false &&
            preg_match_all('#uploads/([A-Za-z0-9._/\-@%]+\.[A-Za-z0-9]{2,5})#', $tekst, $mp)) {
            $paden = $mp[1];
        }
        if (preg_match_all('#\b((?:19|20)\d{2}/\d{2}/[A-Za-z0-9._\-@%]+\.[A-Za-z0-9]{2,5})#', $tekst, $mr)) {
            $paden = array_merge($paden, $mr[1]);
        }
        if ($paden) {
            foreach ($paden as $rel) {
                $rel = rawurldecode($rel);
                $k   = self::sleutel($rel);
                if (isset($this->pathToId[$k])) {
                    $this->refs[$this->pathToId[$k]][$bron] = true;
                    continue;
                }
                $naam = basename($rel);
                if (isset($this->baseToId[self::sleutel($naam)])) {
                    $this->refs[$this->baseToId[self::sleutel($naam)]][$bron] = true;
                    continue;
                }
                $variant = self::zonderFormaat($naam);
                if (isset($this->baseToId[self::sleutel($variant)])) {
                    $this->refs[$this->baseToId[self::sleutel($variant)]][$bron] = true;
                }
            }
        }
    }

    /**
     * collectBare zoekt bekende bestandsnamen in een waarde. Dat is zwakker bewijs
     * dan een pad — een naam kan toeval zijn — dus het krijgt zijn eigen label.
     * Plugins die alleen de bestandsnaam bewaren zijn er genoeg, en die media
     * "ongebruikt" noemen is het risico niet waard.
     */
    private function collectBare($waarde)
    {
        if (!is_string($waarde) || $waarde === '' || strlen($waarde) > 200000) {
            return;
        }
        $kandidaten = [];
        $kort = trim($waarde);
        if (strpos($kort, '/') === false && strlen($kort) <= 200) {
            $kandidaten[] = $kort;
        }
        // Namen mét extensie uit langere tekst; alleen als er een punt in zit, zodat
        // gewone woorden niet meedoen.
        if (preg_match_all('/([A-Za-z0-9][A-Za-z0-9._\-@%]{2,150}\.(?:jpe?g|png|gif|webp|avif|svg|pdf|mp4|mov|tif{1,2}|zip|docx?|xlsx?|pptx?))/i', $waarde, $m)) {
            foreach ($m[1] as $naam) {
                $kandidaten[] = basename($naam);
            }
        }
        foreach (array_unique($kandidaten) as $naam) {
            $k = self::sleutel($naam);
            if (isset($this->baseToId[$k])) {
                $this->refs[$this->baseToId[$k]]['filename_only'] = true;
            }
        }
    }

    /**
     * collectIdMeta pakt attachment-ID's uit meta die geen URL bevat: een los ID
     * (uitgelichte afbeelding), een kommalijst (WooCommerce-galerij) of een
     * geserialiseerde array (ACF-galerij). Zonder dit blijft een webshop met al zijn
     * productfoto's "ongebruikt".
     */
    private function collectIdMeta($key, $waarde, $bron)
    {
        $waarde = trim((string) $waarde);
        if ($waarde === '' || strlen($waarde) > 100000) {
            return;
        }
        $mediaKey = $key === '_thumbnail_id'
            || preg_match('/(image|logo|thumbnail|photo|avatar|icon|file|gallery|galerij|media|attachment|banner|slide|header)/i', $key);
        if (!$mediaKey) {
            return;
        }

        $ids = [];
        if (ctype_digit($waarde)) {
            $ids[] = (int) $waarde;
        } elseif (preg_match('/^[\d,\s]+$/', $waarde)) {
            foreach (preg_split('/[,\s]+/', $waarde) as $deel) {
                if ($deel !== '') {
                    $ids[] = (int) $deel;
                }
            }
        } elseif (strpos($waarde, 'a:') === 0 || strpos($waarde, 'i:') === 0) {
            // Geserialiseerde array: alle gehele getallen eruit halen is ruim, maar
            // fout gaat de veilige kant op — iets ten onrechte "in gebruik" noemen is
            // beter dan iets ten onrechte "ongebruikt".
            if (preg_match_all('/(?:i:|s:\d+:")(\d+)/', $waarde, $m)) {
                foreach ($m[1] as $ruw) {
                    $ids[] = (int) $ruw;
                }
            }
        }

        foreach ($ids as $id) {
            if ($id > 0 && isset($this->byId[$id])) {
                $this->refs[$id][$bron] = true;
            }
        }
    }

    private function collectReferences()
    {
        if (!$this->indexComplete) {
            $this->notes[] = 'referentiescan niet gedraaid: de mediabibliotheek is niet volledig geïndexeerd';
            return;
        }
        try {
            $this->scanPosts();
            $this->scanPostMeta();
            $this->scanOptions();
            $this->scanTermAndUserMeta();
            $this->scanThemeCode();
            $this->scanExtraTables();
            $this->refScanComplete = !$this->overBudget();
            if (!$this->refScanComplete) {
                $this->notes[] = 'tijdsbudget geraakt tijdens het zoeken naar referenties';
            }
        } catch (Throwable $e) {
            $this->refs             = [];
            $this->refScanComplete  = false;
            $this->notes[]          = 'referentiescan afgebroken: ' . $e->getMessage();
        }
    }

    private function scanPosts()
    {
        $this->tables[] = $this->db->posts;
        $laatste = 0;
        while (!$this->overBudget()) {
            $rijen = $this->haalRijen($this->db->prepare(
                "SELECT ID, post_type, post_status, post_content, post_excerpt
                   FROM {$this->db->posts}
                  WHERE ID > %d AND post_type <> 'attachment'
                    AND post_status NOT IN ('auto-draft','trash')
                  ORDER BY ID ASC LIMIT %d",
                $laatste,
                self::BATCH_CONTENT
            ), 'posts');
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                $laatste = (int) $r['ID'];
                // Revisies gelden niet als bewijs dat iets nu in gebruik is, maar
                // wel als waarschuwing dat terugzetten iets kan breken.
                $bron = $r['post_type'] === 'revision' ? 'revision_only' : 'content';
                $this->collect($r['post_content'], $bron);
                $this->collect($r['post_excerpt'], $bron);
            }
        }
    }

    private function scanPostMeta()
    {
        $this->tables[] = $this->db->postmeta;
        $laatste = 0;
        while (!$this->overBudget()) {
            $rijen = $this->haalRijen($this->db->prepare(
                "SELECT meta_id, meta_key, meta_value
                   FROM {$this->db->postmeta}
                  WHERE meta_id > %d
                    AND meta_key NOT IN ('_wp_attached_file','_wp_attachment_metadata','_wp_attachment_backup_sizes')
                  ORDER BY meta_id ASC LIMIT %d",
                $laatste,
                self::BATCH
            ), 'postmeta');
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                $laatste = (int) $r['meta_id'];
                $key     = (string) $r['meta_key'];
                $bron    = substr($key, 0, 1) === '_' ? 'meta' : 'acf';
                $this->collect($r['meta_value'], $bron);
                $this->collectIdMeta($key, $r['meta_value'], $bron);
                $this->collectBare(trim((string) $r['meta_value']));
            }
        }
    }

    private function scanOptions()
    {
        $this->tables[] = $this->db->options;
        $laatste = 0;
        while (!$this->overBudget()) {
            $rijen = $this->haalRijen($this->db->prepare(
                "SELECT option_id, option_name, option_value FROM {$this->db->options}
                  WHERE option_id > %d
                    AND option_name NOT LIKE '\_transient%%' AND option_name NOT LIKE '\_site\_transient%%'
                  ORDER BY option_id ASC LIMIT %d",
                $laatste,
                500
            ), 'options');
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                $laatste = (int) $r['option_id'];
                $this->collect($r['option_value'], 'options');
                $this->collectIdMeta((string) $r['option_name'], $r['option_value'], 'options');
            }
        }
    }

    private function scanTermAndUserMeta()
    {
        foreach ([[$this->db->termmeta, 'meta_id', 'termmeta'], [$this->db->usermeta, 'umeta_id', 'usermeta']] as $t) {
            list($tabel, $pk, $bron) = $t;
            $this->tables[] = $tabel;
            $laatste = 0;
            while (!$this->overBudget()) {
                $rijen = $this->haalRijen($this->db->prepare(
                    "SELECT $pk AS pk, meta_key, meta_value FROM $tabel
                      WHERE $pk > %d AND meta_key NOT LIKE '%%capabilities%%' AND meta_key <> 'session_tokens'
                      ORDER BY $pk ASC LIMIT %d",
                    $laatste,
                    self::BATCH
                ), $bron);
                if (!$rijen) {
                    break;
                }
                foreach ($rijen as $r) {
                    $laatste = (int) $r['pk'];
                    $this->collect($r['meta_value'], $bron);
                    $this->collectIdMeta((string) $r['meta_key'], $r['meta_value'], $bron);
                }
            }
        }
    }

    /**
     * scanExtraTables doorzoekt tekstkolommen van niet-core tabellen met dezelfde
     * prefix. Sliders, formulieren en vastgoedplugins bewaren hun media daar, en dat
     * is de grootste blinde vlek van elke "ongebruikte media"-analyse: zonder deze
     * pass lijkt de halve mediabibliotheek van zo'n site ongebruikt.
     */
    private function scanExtraTables()
    {
        $prefix = (string) $this->db->prefix;
        if ($prefix === '' || $this->overBudget()) {
            return;
        }
        $core = ['posts', 'postmeta', 'options', 'termmeta', 'usermeta', 'users', 'terms',
            'term_taxonomy', 'term_relationships', 'comments', 'commentmeta', 'links'];
        $coreNamen = [];
        foreach ($core as $c) {
            $coreNamen[] = $prefix . $c;
        }

        $tabellen = $this->db->get_col("SHOW TABLES LIKE '" . $this->db->esc_like($prefix) . "%'");
        foreach ((array) $tabellen as $tabel) {
            if ($this->overBudget()) {
                return;
            }
            // Namen komen uit de database zelf, maar ze belanden in SQL zonder
            // placeholder, dus alleen doodgewone identifiers toelaten.
            if (!is_string($tabel) || !preg_match('/^[A-Za-z0-9_]+$/', $tabel) || in_array($tabel, $coreNamen, true)) {
                continue;
            }
            $this->scanEenExtraTabel($tabel);
        }
    }

    private function scanEenExtraTabel($tabel)
    {
        $kolommen = $this->db->get_results("SHOW COLUMNS FROM `$tabel`", ARRAY_A);
        $tekst    = [];
        $pk       = '';
        foreach ((array) $kolommen as $k) {
            $naam = isset($k['Field']) ? (string) $k['Field'] : '';
            $type = isset($k['Type']) ? strtolower((string) $k['Type']) : '';
            if (!preg_match('/^[A-Za-z0-9_]+$/', $naam)) {
                continue;
            }
            if (preg_match('/(char|text|blob|json)/', $type)) {
                $tekst[] = $naam;
            }
            if ($pk === '' && isset($k['Key']) && $k['Key'] === 'PRI' && strpos($type, 'int') !== false) {
                $pk = $naam;
            }
        }
        if (!$tekst) {
            return;
        }

        $this->tables[] = $tabel;
        $kolomLijst = '`' . implode('`,`', $tekst) . '`';
        $gelezen    = 0;
        $laatste    = 0;

        while (!$this->overBudget() && $gelezen < 50000) {
            if ($pk !== '') {
                $sql = $this->db->prepare(
                    "SELECT `$pk` AS rdm_pk, $kolomLijst FROM `$tabel` WHERE `$pk` > %d ORDER BY `$pk` ASC LIMIT %d",
                    $laatste,
                    self::BATCH_CONTENT
                );
            } else {
                // Zonder numerieke primary key kan er niet gepagineerd worden; dan
                // één begrensde greep, en dat staat in de notities.
                $sql = $this->db->prepare("SELECT $kolomLijst FROM `$tabel` LIMIT %d", 5000);
            }
            $rijen = $this->haalRijen($sql, 'extra:' . $tabel);
            if (!$rijen) {
                return;
            }
            foreach ($rijen as $r) {
                if ($pk !== '' && isset($r['rdm_pk'])) {
                    $laatste = (int) $r['rdm_pk'];
                }
                foreach ($tekst as $kolom) {
                    if (!isset($r[$kolom])) {
                        continue;
                    }
                    $this->collect($r[$kolom], 'extra_table');
                    $this->collectBare((string) $r[$kolom]);
                }
                $gelezen++;
            }
            if ($pk === '') {
                $this->notes[] = 'tabel ' . $tabel . ' heeft geen numerieke sleutel; maximaal 5000 rijen bekeken';
                return;
            }
        }
        if ($gelezen >= 50000) {
            $this->notes[] = 'tabel ' . $tabel . ' is groot; eerste 50.000 rijen bekeken';
        }
    }

    private function scanThemeCode()
    {
        $gelezen = 0;
        foreach ([WP_CONTENT_DIR . '/themes', WP_CONTENT_DIR . '/mu-plugins'] as $map) {
            if (!is_dir($map) || $this->overBudget()) {
                continue;
            }
            $it = new RecursiveIteratorIterator(
                new RecursiveDirectoryIterator($map, FilesystemIterator::SKIP_DOTS),
                RecursiveIteratorIterator::LEAVES_ONLY,
                RecursiveIteratorIterator::CATCH_GET_CHILD
            );
            foreach ($it as $f) {
                if (!$f->isFile() || !preg_match('/\.(php|css|js|json|twig|html)$/i', $f->getFilename())) {
                    continue;
                }
                if ($f->getSize() > 2 * 1024 * 1024) {
                    continue;
                }
                $gelezen += $f->getSize();
                $this->themeFiles++;
                $this->collect(file_get_contents($f->getPathname()), 'theme');
                if ($gelezen > 50 * 1024 * 1024 || $this->overBudget()) {
                    $this->notes[] = 'themacode maar deels gelezen (limiet bereikt)';
                    return;
                }
            }
        }
    }

    // --- 5. bibliotheek-items zonder referentie ---

    private function findUnreferenced()
    {
        foreach ($this->byId as $id => $a) {
            if (!isset($this->refs[$id])) {
                continue;
            }
            $this->refCount++;
            $bytes = isset($this->onDisk[self::sleutel($a['file'])]) ? (int) $this->onDisk[self::sleutel($a['file'])] : 0;
            $this->inUseBytes += $bytes;
            if (count($this->inUse) < self::DETAIL_CAP) {
                // Het bewijs mee: zonder "waar is dit gevonden" is een lijst met
                // gebruikte media niet na te lopen.
                $this->inUse[] = [
                    'path' => $a['file'], 'bytes' => $bytes, 'modifiedAt' => $a['date'],
                    'class' => 'original', 'category' => 'in_use',
                    'attachmentId' => $id, 'title' => $a['title'], 'mimeType' => $a['mime'],
                    'evidence' => array_keys($this->refs[$id]),
                ];
            }
        }
        if (!$this->refScanComplete || !$this->walkComplete) {
            return; // geen uitspraak zonder complete referentiescan én bestandslijst
        }
        foreach ($this->byId as $id => $a) {
            if (isset($this->refs[$id]) || isset($this->missingIds[$id])) {
                continue;
            }
            $k     = self::sleutel($a['file']);
            $bytes = isset($this->onDisk[$k]) ? (int) $this->onDisk[$k] : 0;
            $this->unrefCount++;
            $this->unrefBytes += $bytes;
            if (count($this->unref) < self::DETAIL_CAP) {
                $this->unref[] = [
                    'path' => $a['file'], 'bytes' => $bytes, 'modifiedAt' => $a['date'],
                    'class' => 'original', 'category' => 'unreferenced',
                    'attachmentId' => $id, 'title' => $a['title'], 'mimeType' => $a['mime'],
                ];
            }
        }
    }

    // --- 6. uitvoer ---

    private function klasseTotalen()
    {
        $uit = [];
        foreach ($this->byClass as $klasse => $t) {
            // (string) is geen overbodige netheid: PHP maakt van een sleutel die
            // alleen uit cijfers bestaat een int, en dan staat er in de JSON een
            // getal waar de andere kant een string verwacht.
            $uit[] = ['class' => (string) $klasse, 'files' => $t['files'], 'bytes' => $t['bytes']];
        }
        usort($uit, function ($a, $b) {
            return $b['bytes'] <=> $a['bytes'];
        });
        return $uit;
    }

    private function periodeTotalen()
    {
        $uit = [];
        foreach ($this->byPeriod as $periode => $t) {
            // Een map die "2020" heet wordt door PHP een int-sleutel; zonder cast
            // levert json_encode daar een getal voor op.
            $uit[] = ['period' => (string) $periode, 'files' => $t['files'], 'bytes' => $t['bytes']];
        }
        usort($uit, function ($a, $b) {
            return strcmp($a['period'], $b['period']);
        });
        return array_slice($uit, 0, 400);
    }

    private function categorie($naam, $hard, $aantal, $bytes, $rijen)
    {
        return [
            'category'  => $naam,
            'hard'      => $hard,
            'files'     => $aantal,
            'bytes'     => $bytes,
            'samples'   => array_slice($rijen, 0, self::SAMPLE),
            'truncated' => count($rijen) < $aantal,
        ];
    }

    private function payload()
    {
        $volledig = $this->indexComplete && $this->walkComplete && $this->refScanComplete;

        return [
            'folders'           => $this->folders,
            'uploadsPath'       => $this->basedir,
            'uploadsUrl'        => $this->baseurl,
            'multisite'         => is_multisite(),
            'totalFiles'        => $this->totalFiles,
            'totalBytes'        => $this->totalBytes,
            'attachmentCount'   => count($this->byId),
            'referencedCount'   => $this->refCount,
            'byClass'           => $this->klasseTotalen(),
            'byPeriod'          => $this->periodeTotalen(),
            'largest'           => $this->largest,
            'categories'        => [
                $this->categorie('in_use', true, $this->refCount, $this->inUseBytes, $this->inUse),
                $this->categorie('unreferenced', false, $this->unrefCount, $this->unrefBytes, $this->unref),
                $this->categorie('orphan_file', true, $this->orphanCount, $this->orphanBytes, $this->orphans),
                $this->categorie('missing_file', true, $this->missingCount, 0, $this->missing),
            ],
            'detail'            => array_merge($this->inUse, $this->unref, $this->orphans, $this->missing),
            'tablesScanned'     => array_values(array_unique($this->tables)),
            'rowsScanned'       => $this->rowsScanned,
            'themeFilesScanned' => $this->themeFiles,
            'referenceScanRan'  => $this->refScanComplete,
            'indexComplete'     => $this->indexComplete,
            'walkComplete'      => $this->walkComplete,
            'offloadDetected'   => $this->offload,
            'truncated'         => !$volledig,
            'durationMs'        => (int) round((microtime(true) - $this->start) * 1000),
            'notes'             => $this->notes,
        ];
    }
}

} // class_exists

// $wpdb is een echte WordPress-global en blijft dus bereikbaar, ook binnen een
// functiescope. Het budget komt uit de omgeving; zonder waarde ruim genomen.
$rdmDb = isset($wpdb) ? $wpdb : $GLOBALS['wpdb'];
$rdmBudget = getenv('RDM_MEDIA_BUDGET');
// De mapselectie komt base64-gecodeerd binnen: mapnamen kunnen spaties en
// aanhalingstekens bevatten, en die mogen de shell niet raken.
$rdmFolders = [];
$rdmFoldersRaw = getenv('RDM_MEDIA_FOLDERS');
if ($rdmFoldersRaw !== false && $rdmFoldersRaw !== '') {
    $rdmFolders = array_filter(explode("\n", (string) base64_decode($rdmFoldersRaw, true)), 'strlen');
}
$rdmScan = new RdmMediaScan(
    $rdmDb,
    $rdmBudget !== false && $rdmBudget !== '' ? (float) $rdmBudget : 1800.0,
    $rdmFolders
);
rdm_emit($rdmScan->run());
