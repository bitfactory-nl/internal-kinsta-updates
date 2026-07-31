<?php
/**
 * Stub-WordPress zodat media_scan.php lokaal getest kan worden, zonder site en
 * zonder database. De fixture komt als JSON binnen via RDM_TEST_FIXTURE; de
 * uploads-map is een echte tijdelijke map die de Go-test aanmaakt.
 *
 * Alleen voor tests: dit bestand hoort niet op een server.
 */

$fixturePad = getenv('RDM_TEST_FIXTURE');
if (!$fixturePad || !is_file($fixturePad)) {
    fwrite(STDERR, "RDM_TEST_FIXTURE ontbreekt\n");
    exit(1);
}
$fixture = json_decode(file_get_contents($fixturePad), true);

define('ABSPATH', $fixture['content'] . '/');
define('WP_CONTENT_DIR', $fixture['content']);
define('ARRAY_A', 'ARRAY_A');
define('OBJECT', 'OBJECT');

function wp_get_upload_dir()
{
    global $fixture;
    return ['basedir' => $fixture['uploads'], 'baseurl' => 'https://voorbeeld.test/wp-content/uploads'];
}

function is_multisite()
{
    return false;
}

/** Fake $wpdb die de queries van het scanscript herkent op kenmerkende tekst. */
class RdmFakeWpdb
{
    public $posts    = 'wp_posts';
    public $postmeta = 'wp_postmeta';
    public $options  = 'wp_options';
    public $termmeta = 'wp_termmeta';
    public $usermeta = 'wp_usermeta';
    public $prefix   = 'wp_';

    private $fx;

    public function __construct(array $fx)
    {
        $this->fx = $fx;
    }

    public function prepare($query, ...$args)
    {
        return vsprintf($query, $args);
    }

    public function get_var($query)
    {
        if (strpos($query, 'COUNT(*)') !== false && strpos($query, 'wp_options') !== false) {
            return (string) ($this->fx['offload'] ?? 0);
        }
        return '0';
    }

    public function get_results($query, $mode = null)
    {
        list($rows, $pk) = $this->bron($query);
        $na    = $this->getal($query, '/>\s*(\d+)/');
        $limit = $this->getal($query, '/LIMIT\s+(\d+)/') ?: 1000;

        $out = [];
        foreach ($rows as $r) {
            if ($pk !== null && (int) $r[$pk] <= $na) {
                continue;
            }
            $out[] = $r;
            if (count($out) >= $limit) {
                break;
            }
        }
        return $out;
    }

    /** Welke fixture-lijst hoort bij deze query, en op welke kolom loopt de cursor? */
    private function bron($query)
    {
        // Let op de volgorde: de gewone postmeta-query noemt óók de meta_keys die
        // ze uitsluit, dus die moet eerst worden herkend.
        if (strpos($query, 'meta_key NOT IN') !== false) {
            return [$this->fx['postmeta'] ?? [], 'meta_id'];
        }
        if (strpos($query, "meta_key IN ('_wp_attachment_metadata'") !== false) {
            return [$this->fx['attachmentMeta'] ?? [], 'meta_id'];
        }
        if (strpos($query, '_wp_attached_file') !== false) {
            return [$this->fx['attachments'] ?? [], 'ID'];
        }
        if (strpos($query, "post_type <> 'attachment'") !== false) {
            return [$this->fx['posts'] ?? [], 'ID'];
        }
        if (strpos($query, 'wp_options') !== false) {
            return [$this->fx['options'] ?? [], 'option_id'];
        }
        if (strpos($query, 'wp_termmeta') !== false) {
            return [$this->fx['termmeta'] ?? [], 'pk'];
        }
        if (strpos($query, 'wp_usermeta') !== false) {
            return [$this->fx['usermeta'] ?? [], 'pk'];
        }
        return [[], null];
    }

    private function getal($query, $patroon)
    {
        return preg_match($patroon, $query, $m) ? (int) $m[1] : 0;
    }
}

$wpdb = new RdmFakeWpdb($fixture);

// Ruis vóór het resultaat: op een echte site printen WP-CLI en plugins dit ook,
// en de parser moet er niet op omvallen.
echo "PHP Warning: testruis op stdout\n";

// BELANGRIJK: includen BINNEN een functie, precies zoals `wp eval-file` doet.
// Op globaal niveau includen verbergt juist de fout die dit script ooit maakte —
// top-level variabelen zijn hier lokaal, dus `global $x` levert niets op.
function rdm_include_zoals_wp_cli($pad)
{
    include $pad;
}

rdm_include_zoals_wp_cli(dirname(__DIR__) . '/media_scan.php');
