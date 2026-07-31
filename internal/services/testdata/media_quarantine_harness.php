<?php
/**
 * Stub-WordPress voor het quarantainescript: geen database nodig, alleen een
 * uploads-map. Includeert binnen een functie, net als `wp eval-file`.
 */
$fixturePad = getenv('RDM_TEST_FIXTURE');
if (!$fixturePad || !is_file($fixturePad)) {
    fwrite(STDERR, "RDM_TEST_FIXTURE ontbreekt\n");
    exit(1);
}
$fixture = json_decode(file_get_contents($fixturePad), true);

define('ABSPATH', $fixture['content'] . '/');

function wp_get_upload_dir()
{
    global $fixture;
    return ['basedir' => $fixture['uploads'], 'baseurl' => 'https://voorbeeld.test/wp-content/uploads'];
}

echo "PHP Warning: testruis op stdout\n";

function rdm_include_zoals_wp_cli($pad)
{
    include $pad;
}

rdm_include_zoals_wp_cli(dirname(__DIR__) . '/media_quarantine.php');
