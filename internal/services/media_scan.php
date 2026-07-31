<?php
/**
 * RDM media scan — READ-ONLY analyse van wp-content/uploads.
 *
 * Draait via `wp eval-file` op de server, zodat de zware kant (honderdduizenden
 * bestanden, honderden MB content) bij de data blijft en er één compacte JSON
 * over de SSH-verbinding gaat.
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

global $wpdb;

$RDM_START  = microtime(true);
$RDM_BUDGET = (float) (getenv('RDM_MEDIA_BUDGET') ?: 330); // seconden; ruim onder de Go-timeout
$RDM_BATCH  = 2000;
$RDM_TOP    = 100;   // grootste bestanden
$RDM_SAMPLE = 500;   // voorbeeldrijen per categorie in de samenvatting
$RDM_DETAIL = 20000; // detailrijen per categorie

$notes     = [];
$truncated = false;

/** Tijdsbudget op: verder gaan levert alleen een timeout op in plaats van een antwoord. */
function rdm_over_budget()
{
    global $RDM_START, $RDM_BUDGET;
    return (microtime(true) - $RDM_START) > $RDM_BUDGET;
}

/** 64-bits sleutel: int-keys houden een index van 300k paden rond de 10 MB in plaats van 90 MB. */
function rdm_key($s)
{
    $u = unpack('J', substr(md5($s, true), 0, 8));
    return $u[1];
}

/** Formaatsuffixen strippen: -300x200, -scaled, @2x, -e1712345678. */
function rdm_base_variant($name)
{
    $name = preg_replace('/-e\d{9,}(?=\.[A-Za-z0-9]+$)/', '', $name);
    $name = preg_replace('/-\d+x\d+(?=\.[A-Za-z0-9]+$)/', '', $name);
    $name = preg_replace('/-scaled(?=\.[A-Za-z0-9]+$)/', '', $name);
    $name = preg_replace('/@2x(?=\.[A-Za-z0-9]+$)/', '', $name);
    return $name;
}

// ---------------------------------------------------------------------------
// 1. Uploads-map volgens WordPress zelf (respecteert WP_CONTENT_DIR, multisite
//    en upload-dir-filters van offload-plugins).
// ---------------------------------------------------------------------------

$updir    = wp_get_upload_dir();
$basedir  = rtrim($updir['basedir'], '/');
$baseurl  = $updir['baseurl'];
$prefixLn = strlen($basedir) + 1;

if (!is_dir($basedir)) {
    rdm_emit(['error' => "uploads-map bestaat niet: $basedir"]);
    exit(0);
}

// ---------------------------------------------------------------------------
// 2. Index van de mediabibliotheek: pad -> attachment, plus alle gegenereerde
//    formaten. Batches op de primary key; nooit OFFSET, nooit WP_Query.
// ---------------------------------------------------------------------------

$byId       = [];  // id => ['file','title','mime','date']
$pathToId   = [];  // rdm_key(relpad) => id
$baseToId   = [];  // rdm_key(basename) => id
$attCount   = 0;

$lastId = 0;
while (true) {
    $rows = $wpdb->get_results($wpdb->prepare(
        "SELECT p.ID, p.post_title, p.post_mime_type, p.post_date_gmt, pm.meta_value AS file
           FROM {$wpdb->posts} p
           JOIN {$wpdb->postmeta} pm ON pm.post_id = p.ID AND pm.meta_key = '_wp_attached_file'
          WHERE p.post_type = 'attachment' AND p.ID > %d
          ORDER BY p.ID ASC LIMIT %d",
        $lastId,
        $RDM_BATCH
    ), ARRAY_A);
    if (!$rows) {
        break;
    }
    foreach ($rows as $r) {
        $id     = (int) $r['ID'];
        $lastId = $id;
        $rel    = ltrim((string) $r['file'], '/');
        if ($rel === '') {
            continue;
        }
        $attCount++;
        $byId[$id] = [
            'file'  => $rel,
            'title' => (string) $r['post_title'],
            'mime'  => (string) $r['post_mime_type'],
            'date'  => (int) strtotime((string) $r['post_date_gmt'] . ' UTC'),
        ];
        $pathToId[rdm_key($rel)] = $id;
        $baseToId[rdm_key(basename($rel))] = $id;
    }
    if (rdm_over_budget()) {
        $truncated = true;
        $notes[]   = 'tijdsbudget geraakt tijdens het indexeren van de mediabibliotheek';
        break;
    }
}

// Gegenereerde formaten, -scaled originelen en editor-backups toevoegen. De
// geserialiseerde meta wordt met een regex uitgelezen: geen unserialize, dus geen
// tijdelijke arrays per attachment.
$lastMeta = 0;
while (!$truncated) {
    $rows = $wpdb->get_results($wpdb->prepare(
        "SELECT meta_id, post_id, meta_value
           FROM {$wpdb->postmeta}
          WHERE meta_key IN ('_wp_attachment_metadata','_wp_attachment_backup_sizes')
            AND meta_id > %d
          ORDER BY meta_id ASC LIMIT %d",
        $lastMeta,
        $RDM_BATCH
    ), ARRAY_A);
    if (!$rows) {
        break;
    }
    foreach ($rows as $r) {
        $lastMeta = (int) $r['meta_id'];
        $id       = (int) $r['post_id'];
        if (!isset($byId[$id])) {
            continue;
        }
        $dir = trim(dirname($byId[$id]['file']), '.');
        $dir = $dir === '' ? '' : rtrim($dir, '/') . '/';
        if (preg_match_all('/"file";s:\d+:"([^"]+)"/', (string) $r['meta_value'], $m)) {
            foreach ($m[1] as $f) {
                $pathToId[rdm_key($dir . $f)] = $id;
                $baseToId[rdm_key(basename($f))] = $id;
            }
        }
        if (preg_match('/"original_image";s:\d+:"([^"]+)"/', (string) $r['meta_value'], $mo)) {
            $pathToId[rdm_key($dir . $mo[1])] = $id;
            $baseToId[rdm_key(basename($mo[1]))] = $id;
        }
    }
    if (rdm_over_budget()) {
        $truncated = true;
        $notes[]   = 'tijdsbudget geraakt tijdens het indexeren van afbeeldingsformaten';
        break;
    }
}

// ---------------------------------------------------------------------------
// 3. Bestanden op schijf: groottes, klassen en categorie A (niet in de bibliotheek).
// ---------------------------------------------------------------------------

/** Mappen die plugins gebruiken voor caches, exports en backups: geen media. */
function rdm_is_system_path($rel)
{
    static $needles = [
        'cache/', 'wp-rocket/', 'wpo-cache/', 'litespeed/', 'elementor/css/', 'et-cache/',
        'wpforms/', 'gravity_forms/', 'wpcf7_uploads/', 'formidable/', 'ninja-forms/',
        'wp-personal-data-exports/', 'backwpup', 'updraft', 'wp-migrate-db/', 'wpvivid',
        'backup', 'woocommerce_uploads/', 'wc-logs/', 'shortpixel-backups/', 'ewww/',
        'smush-webp/', 'sitemaps/', 'wp-statistics/',
    ];
    $l = strtolower($rel);
    foreach ($needles as $n) {
        if (strpos($l, $n) !== false) {
            return true;
        }
    }
    return preg_match('/\.(zip|gz|tar|sql|log|bak|tmp|txt|json|xml|css|js)$/i', $l) === 1;
}

/** Klasse van een bestand: wat heeft het gemaakt? */
function rdm_classify($rel, $known, $baseToId)
{
    $name = basename($rel);
    if (rdm_is_system_path($rel)) {
        return 'system';
    }
    if (preg_match('/-e\d{9,}\.[A-Za-z0-9]+$/', $name)) {
        return 'editor_backup';
    }
    if (preg_match('/\.(webp|avif)$/i', $name)) {
        // Alleen een next-gen variant als er een origineel naast ligt.
        $stripped = preg_replace('/\.(webp|avif)$/i', '', $name);
        if (isset($baseToId[rdm_key($stripped)]) || strpos($stripped, '.') !== false) {
            return 'nextgen';
        }
    }
    if (preg_match('/-scaled\.[A-Za-z0-9]+$/', $name)) {
        return 'scaled';
    }
    if (preg_match('/-\d+x\d+\.[A-Za-z0-9]+$/', $name)) {
        return 'generated';
    }
    return $known ? 'original' : 'unknown';
}

$totalFiles = 0;
$totalBytes = 0;
$byClass    = [];   // klasse => ['files','bytes']
$byPeriod   = [];   // "2024/05" => ['files','bytes']
$largest    = [];   // gesorteerd bijgehouden, max $RDM_TOP
$orphans    = [];   // categorie A
$orphanCnt  = 0;
$orphanByte = 0;
$onDisk     = [];   // rdm_key(relpad) => bytes, voor categorie B en C

$it = new RecursiveIteratorIterator(
    new RecursiveDirectoryIterator($basedir, FilesystemIterator::SKIP_DOTS),
    RecursiveIteratorIterator::LEAVES_ONLY,
    RecursiveIteratorIterator::CATCH_GET_CHILD
);

foreach ($it as $file) {
    if (!$file->isFile()) {
        continue;
    }
    $rel = substr($file->getPathname(), $prefixLn);
    if ($rel === false || $rel === '') {
        continue;
    }
    $bytes = (int) $file->getSize();
    $mtime = (int) $file->getMTime();

    $totalFiles++;
    $totalBytes += $bytes;
    $onDisk[rdm_key($rel)] = $bytes;

    $id    = isset($pathToId[rdm_key($rel)]) ? $pathToId[rdm_key($rel)] : 0;
    $class = rdm_classify($rel, $id > 0, $baseToId);

    if (!isset($byClass[$class])) {
        $byClass[$class] = ['files' => 0, 'bytes' => 0];
    }
    $byClass[$class]['files']++;
    $byClass[$class]['bytes'] += $bytes;

    $parts  = explode('/', $rel);
    $period = (count($parts) >= 3 && preg_match('/^\d{4}$/', $parts[0]) && preg_match('/^\d{2}$/', $parts[1]))
        ? $parts[0] . '/' . $parts[1]
        : 'overig/' . $parts[0];
    if (!isset($byPeriod[$period])) {
        $byPeriod[$period] = ['files' => 0, 'bytes' => 0];
    }
    $byPeriod[$period]['files']++;
    $byPeriod[$period]['bytes'] += $bytes;

    if (count($largest) < $RDM_TOP) {
        $largest[] = ['path' => $rel, 'bytes' => $bytes, 'modifiedAt' => $mtime, 'class' => $class, 'attachmentId' => $id];
        if (count($largest) === $RDM_TOP) {
            // Vanaf hier op grootte gesorteerd houden, zodat het laatste element
            // altijd de kleinste is en één vergelijking per bestand volstaat.
            usort($largest, function ($a, $b) { return $b['bytes'] <=> $a['bytes']; });
        }
    } elseif ($bytes > $largest[count($largest) - 1]['bytes']) {
        $largest[count($largest) - 1] = ['path' => $rel, 'bytes' => $bytes, 'modifiedAt' => $mtime, 'class' => $class, 'attachmentId' => $id];
        usort($largest, function ($a, $b) { return $b['bytes'] <=> $a['bytes']; });
    }

    // Categorie A: niet in de bibliotheek, en ook niet herkenbaar als afgeleide
    // van een bekend bestand (thumbnail, webp-variant, editor-backup) of als
    // bekende plugin-rommel. Die laatste twee tellen wél mee in de groottes,
    // maar horen niet in een lijst die "staat niet in de mediabibliotheek" heet.
    if ($id === 0 && !in_array($class, ['generated', 'nextgen', 'editor_backup', 'scaled', 'system'], true)) {
        $variant = rdm_base_variant(basename($rel));
        if (!isset($baseToId[rdm_key($variant)])) {
            $orphanCnt++;
            $orphanByte += $bytes;
            if (count($orphans) < $RDM_DETAIL) {
                $orphans[] = [
                    'path' => $rel, 'bytes' => $bytes, 'modifiedAt' => $mtime,
                    'class' => $class, 'category' => 'orphan_file',
                ];
            }
        }
    }

    if (($totalFiles % 5000) === 0 && rdm_over_budget()) {
        $truncated = true;
        $notes[]   = 'tijdsbudget geraakt tijdens het doorlopen van de bestanden';
        break;
    }
}
usort($largest, function ($a, $b) { return $b['bytes'] <=> $a['bytes']; });

// ---------------------------------------------------------------------------
// 4. Categorie B: bibliotheek-item waarvan het hoofdbestand mist.
// ---------------------------------------------------------------------------

$missing    = [];
$missingCnt = 0;
$missingIds = [];
foreach ($byId as $id => $a) {
    if (isset($onDisk[rdm_key($a['file'])])) {
        continue;
    }
    $missingCnt++;
    $missingIds[$id] = true;
    if (count($missing) < $RDM_DETAIL) {
        $missing[] = [
            'path' => $a['file'], 'bytes' => 0, 'modifiedAt' => $a['date'],
            'class' => 'unknown', 'category' => 'missing_file',
            'attachmentId' => $id, 'title' => $a['title'], 'mimeType' => $a['mime'],
        ];
    }
}

// Offload-plugins halen bestanden weg bij de server; dan is categorie B ruis.
$offload = (int) $wpdb->get_var(
    "SELECT COUNT(*) FROM {$wpdb->options}
      WHERE option_name IN ('tantan_wordpress_s3','as3cf_settings','wpos3_settings')"
) > 0;
if ($offload) {
    $notes[] = 'offload-plugin gevonden: ontbrekende bestanden staan waarschijnlijk in externe opslag';
}

// ---------------------------------------------------------------------------
// 5. Referenties zoeken. Eén pass over alle content; per bron een bewijs-bit.
//    Faalt dit deel, dan blijven de harde categorieën A en B geldig.
// ---------------------------------------------------------------------------

$refs   = [];  // attachment-id => ['content' => true, ...]
$tables = [];
$themeFiles = 0;

/** Zet het bewijs-bit voor elk attachment waar deze tekst naar verwijst. */
function rdm_collect($text, $bron)
{
    global $refs, $pathToId, $baseToId, $byId;
    if ($text === null || $text === '' || !is_string($text)) {
        return;
    }

    // ID-tokens.
    if (preg_match_all('/(?:wp-image-|wp-att-|attachment[_-]|"id"\s*:\s*|attachment_id"?[:=]\s*"?)(\d+)/i', $text, $m)) {
        foreach ($m[1] as $raw) {
            $id = (int) $raw;
            if (isset($byId[$id])) {
                $refs[$id][$bron] = true;
            }
        }
    }
    if (preg_match_all('/\[gallery[^\]]*ids="([\d,\s]+)"/i', $text, $mg)) {
        foreach ($mg[1] as $list) {
            foreach (preg_split('/[,\s]+/', $list) as $raw) {
                $id = (int) $raw;
                if ($id > 0 && isset($byId[$id])) {
                    $refs[$id][$bron] = true;
                }
            }
        }
    }

    // Pad-tokens: elke verwijzing naar iets in uploads/.
    if (strpos($text, 'uploads/') !== false &&
        preg_match_all('#uploads/([A-Za-z0-9._/\-@%]+\.[A-Za-z0-9]{2,5})#', $text, $mp)) {
        foreach ($mp[1] as $rel) {
            $rel = rawurldecode($rel);
            $k   = rdm_key($rel);
            if (isset($pathToId[$k])) {
                $refs[$pathToId[$k]][$bron] = true;
                continue;
            }
            $name = basename($rel);
            if (isset($baseToId[rdm_key($name)])) {
                $refs[$baseToId[rdm_key($name)]][$bron] = true;
                continue;
            }
            $variant = rdm_base_variant($name);
            if (isset($baseToId[rdm_key($variant)])) {
                $refs[$baseToId[rdm_key($variant)]][$bron] = true;
            }
        }
    }
}

/** Een losse waarde die precies een bekende bestandsnaam is, geldt als zwak bewijs. */
function rdm_collect_bare($value)
{
    global $refs, $baseToId;
    if (!is_string($value) || $value === '' || strlen($value) > 200 || strpos($value, '/') !== false) {
        return;
    }
    $k = rdm_key($value);
    if (isset($baseToId[$k])) {
        $refs[$baseToId[$k]]['filename_only'] = true;
    }
}

try {
    // 5a. Posts: content en excerpt. Revisies apart, want ze zijn geen bewijs dat
    //     iets nu in gebruik is — maar wel dat terugzetten iets kan breken.
    $tables[] = $wpdb->posts;
    $lastId   = 0;
    while (!rdm_over_budget()) {
        $rows = $wpdb->get_results($wpdb->prepare(
            "SELECT ID, post_type, post_status, post_content, post_excerpt
               FROM {$wpdb->posts}
              WHERE ID > %d AND post_type <> 'attachment'
                AND post_status NOT IN ('auto-draft','trash')
              ORDER BY ID ASC LIMIT %d",
            $lastId,
            $RDM_BATCH
        ), ARRAY_A);
        if (!$rows) {
            break;
        }
        foreach ($rows as $r) {
            $lastId = (int) $r['ID'];
            $bron   = $r['post_type'] === 'revision' ? 'revision_only' : 'content';
            rdm_collect($r['post_content'], $bron);
            rdm_collect($r['post_excerpt'], $bron);
        }
    }

    // 5b. Postmeta, zonder de eigen administratie van attachments (die zou elke
    //     attachment naar zichzelf laten verwijzen).
    $tables[] = $wpdb->postmeta;
    $lastMeta = 0;
    while (!rdm_over_budget()) {
        $rows = $wpdb->get_results($wpdb->prepare(
            "SELECT meta_id, meta_key, meta_value
               FROM {$wpdb->postmeta}
              WHERE meta_id > %d
                AND meta_key NOT IN ('_wp_attached_file','_wp_attachment_metadata','_wp_attachment_backup_sizes')
              ORDER BY meta_id ASC LIMIT %d",
            $lastMeta,
            $RDM_BATCH
        ), ARRAY_A);
        if (!$rows) {
            break;
        }
        foreach ($rows as $r) {
            $lastMeta = (int) $r['meta_id'];
            $key      = (string) $r['meta_key'];
            $bron     = ($key === '_thumbnail_id' || substr($key, 0, 1) === '_') ? 'meta' : 'acf';
            rdm_collect($r['meta_value'], $bron);
            if (ctype_digit(trim((string) $r['meta_value']))) {
                $id = (int) $r['meta_value'];
                if (isset($byId[$id]) && ($key === '_thumbnail_id' || strpos($key, 'image') !== false ||
                    strpos($key, 'logo') !== false || strpos($key, 'photo') !== false || strpos($key, 'file') !== false)) {
                    $refs[$id][$bron] = true;
                }
            }
            rdm_collect_bare(trim((string) $r['meta_value']));
        }
    }

    // 5c. Options, zonder transients (ruis en verouderd). Gebatcht, want op sommige
    //     sites is deze tabel tientallen MB's aan autoload-rommel.
    $tables[] = $wpdb->options;
    $lastOpt  = 0;
    while (!rdm_over_budget()) {
        $rows = $wpdb->get_results($wpdb->prepare(
            "SELECT option_id, option_name, option_value FROM {$wpdb->options}
              WHERE option_id > %d
                AND option_name NOT LIKE '\_transient%%' AND option_name NOT LIKE '\_site\_transient%%'
              ORDER BY option_id ASC LIMIT %d",
            $lastOpt,
            500
        ), ARRAY_A);
        if (!$rows) {
            break;
        }
        foreach ($rows as $r) {
            $lastOpt = (int) $r['option_id'];
            rdm_collect($r['option_value'], 'options');
            if (ctype_digit(trim((string) $r['option_value']))) {
                $id = (int) $r['option_value'];
                if (isset($byId[$id]) && preg_match('/(logo|image|icon|thumbnail|avatar)/i', (string) $r['option_name'])) {
                    $refs[$id]['options'] = true;
                }
            }
        }
    }

    // 5d. Term- en usermeta: alleen ID's en URL's, nooit de waarden zelf bewaren.
    foreach ([[$wpdb->termmeta, 'meta_id', 'termmeta'], [$wpdb->usermeta, 'umeta_id', 'usermeta']] as $t) {
        list($table, $pk, $bron) = $t;
        $tables[] = $table;
        $last     = 0;
        while (!rdm_over_budget()) {
            $rows = $wpdb->get_results($wpdb->prepare(
                "SELECT $pk AS pk, meta_key, meta_value FROM $table
                  WHERE $pk > %d AND meta_key NOT LIKE '%%capabilities%%' AND meta_key <> 'session_tokens'
                  ORDER BY $pk ASC LIMIT %d",
                $last,
                $RDM_BATCH
            ), ARRAY_A);
            if (!$rows) {
                break;
            }
            foreach ($rows as $r) {
                $last = (int) $r['pk'];
                rdm_collect($r['meta_value'], $bron);
                if (ctype_digit(trim((string) $r['meta_value']))) {
                    $id = (int) $r['meta_value'];
                    if (isset($byId[$id]) && preg_match('/(image|logo|thumbnail|photo|avatar|file)/i', (string) $r['meta_key'])) {
                        $refs[$id][$bron] = true;
                    }
                }
            }
        }
    }

    // 5e. Thema- en mu-plugin-code: media die daar hard in staat, is in gebruik.
    foreach ([WP_CONTENT_DIR . '/themes', WP_CONTENT_DIR . '/mu-plugins'] as $dir) {
        if (!is_dir($dir) || rdm_over_budget()) {
            continue;
        }
        $codeIt = new RecursiveIteratorIterator(
            new RecursiveDirectoryIterator($dir, FilesystemIterator::SKIP_DOTS),
            RecursiveIteratorIterator::LEAVES_ONLY,
            RecursiveIteratorIterator::CATCH_GET_CHILD
        );
        $gelezen = 0;
        foreach ($codeIt as $f) {
            if (!$f->isFile() || !preg_match('/\.(php|css|js|json|twig|html)$/i', $f->getFilename())) {
                continue;
            }
            if ($f->getSize() > 2 * 1024 * 1024) {
                continue;
            }
            $gelezen += $f->getSize();
            $themeFiles++;
            rdm_collect(file_get_contents($f->getPathname()), 'theme');
            if ($gelezen > 50 * 1024 * 1024 || rdm_over_budget()) {
                $notes[] = 'themacode maar deels gelezen (limiet bereikt)';
                break;
            }
        }
    }
} catch (Throwable $e) {
    $notes[] = 'referentiescan afgebroken: ' . $e->getMessage();
    $refs    = [];
}

// ---------------------------------------------------------------------------
// 6. Categorie C: bibliotheek-items zonder gevonden referentie.
// ---------------------------------------------------------------------------

$unref    = [];
$unrefCnt = 0;
$unrefByte = 0;
$refCount = 0;
$refsWerkten = !empty($refs);

foreach ($byId as $id => $a) {
    if (isset($refs[$id])) {
        $refCount++;
        continue;
    }
    if (!$refsWerkten) {
        continue; // zonder werkende referentiescan geen uitspraak doen
    }
    if (isset($missingIds[$id])) {
        // Bestand is al weg: dat is het harde feit uit categorie B. Hem ook als
        // "geen referentie gevonden" tonen maakt die lijst alleen troebeler.
        continue;
    }
    // Grootte komt uit de eerdere doorloop; opnieuw stat'en zou tienduizenden
    // syscalls kosten voor informatie die we al hebben.
    $k     = rdm_key($a['file']);
    $bytes = isset($onDisk[$k]) ? (int) $onDisk[$k] : 0;
    $unrefCnt++;
    $unrefByte += $bytes;
    if (count($unref) < $RDM_DETAIL) {
        $unref[] = [
            'path' => $a['file'], 'bytes' => $bytes, 'modifiedAt' => $a['date'],
            'class' => 'original', 'category' => 'unreferenced',
            'attachmentId' => $id, 'title' => $a['title'], 'mimeType' => $a['mime'],
        ];
    }
}

// ---------------------------------------------------------------------------
// 7. Uitvoer.
// ---------------------------------------------------------------------------

function rdm_class_totals($byClass)
{
    $out = [];
    foreach ($byClass as $class => $t) {
        $out[] = ['class' => $class, 'files' => $t['files'], 'bytes' => $t['bytes']];
    }
    usort($out, function ($a, $b) { return $b['bytes'] <=> $a['bytes']; });
    return $out;
}

function rdm_period_totals($byPeriod)
{
    $out = [];
    foreach ($byPeriod as $period => $t) {
        $out[] = ['period' => $period, 'files' => $t['files'], 'bytes' => $t['bytes']];
    }
    usort($out, function ($a, $b) { return strcmp($a['period'], $b['period']); });
    return array_slice($out, 0, 400);
}

function rdm_category($naam, $hard, $files, $bytes, $rows, $sample)
{
    return [
        'category'  => $naam,
        'hard'      => $hard,
        'files'     => $files,
        'bytes'     => $bytes,
        'samples'   => array_slice($rows, 0, $sample),
        'truncated' => count($rows) < $files,
    ];
}

function rdm_emit($payload)
{
    $json = json_encode($payload, JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
    echo "\n<<<RDM-MEDIA-1>>>\n";
    echo base64_encode(gzencode($json, 6));
    echo "\n<<<END-RDM-MEDIA-1>>>\n";
}

rdm_emit([
    'uploadsPath'       => $basedir,
    'uploadsUrl'        => $baseurl,
    'multisite'         => is_multisite(),
    'totalFiles'        => $totalFiles,
    'totalBytes'        => $totalBytes,
    'attachmentCount'   => $attCount,
    'referencedCount'   => $refCount,
    'byClass'           => rdm_class_totals($byClass),
    'byPeriod'          => rdm_period_totals($byPeriod),
    'largest'           => $largest,
    'categories'        => [
        rdm_category('orphan_file', true, $orphanCnt, $orphanByte, $orphans, $RDM_SAMPLE),
        rdm_category('missing_file', true, $missingCnt, 0, $missing, $RDM_SAMPLE),
        rdm_category('unreferenced', false, $unrefCnt, $unrefByte, $unref, $RDM_SAMPLE),
    ],
    'detail'            => array_merge($orphans, $missing, $unref),
    'tablesScanned'     => array_values(array_unique($tables)),
    'themeFilesScanned' => $themeFiles,
    'referenceScanRan'  => $refsWerkten,
    'offloadDetected'   => $offload,
    'truncated'         => $truncated,
    'durationMs'        => (int) round((microtime(true) - $RDM_START) * 1000),
    'notes'             => $notes,
]);
