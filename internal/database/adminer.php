<?php
// Every request must come through the authenticated panel reverse proxy.
$panelGatewayKey = getenv('PANEL_GATEWAY_KEY');
if (!$panelGatewayKey || !hash_equals($panelGatewayKey, $_SERVER['HTTP_X_PANEL_GATEWAY'] ?? '')) {
    http_response_code(403);
    exit('Open Adminer from the panel.');
}
header('Cache-Control: no-store');
$panelConnection = json_decode(base64_decode($_SERVER['HTTP_X_PANEL_CONNECTION'] ?? ''), true);
if (!is_array($panelConnection) || empty($panelConnection['password'])) {
    http_response_code(403);
    exit('Missing connection.');
}
// Pin the connection to the instance selected in the panel, including crafted requests.
$panelDriver = $panelConnection['driver'];
unset($_GET['server'], $_GET['pgsql'], $_GET['sqlite'], $_GET['oracle'], $_GET['mssql'], $_GET['odbc'], $_GET['elastic'], $_GET['mongo'], $_GET['simpledb'], $_GET['firebird']);
$_GET[$panelDriver] = $panelConnection['server'];
$_GET['username'] = $panelConnection['username'];
if (!isset($_GET['db'])) $_GET['db'] = $panelConnection['database'];
unset($_POST['auth']);
// Configure cookies before loading Adminer, but let its bootstrap open its own
// adminer_sid session. Starting one here prevents Adminer from configuring PHP's
// session settings and can send warning output before its response headers.
ini_set('session.use_strict_mode', '1');
ini_set('session.cookie_samesite', 'Strict');
ini_set('session.cookie_secure', ($_SERVER['HTTP_X_PANEL_HTTPS'] ?? '') === '1' ? '1' : '0');
$_SERVER['PHP_SELF'] = $_SERVER['HTTP_X_PANEL_PATH'];
$_SERVER['SCRIPT_NAME'] = $_SERVER['HTTP_X_PANEL_PATH'];
$_SERVER['REQUEST_URI'] = $_SERVER['HTTP_X_PANEL_PATH'] . (empty($_SERVER['QUERY_STRING']) ? '' : '?' . $_SERVER['QUERY_STRING']);
function adminer_object() {
    class PanelAdminer extends Adminer\Adminer {
        function credentials() {
            global $panelConnection;
            return [$panelConnection['server'], $panelConnection['username'], $panelConnection['password']];
        }
        function login($login, $password) { return true; }
        function permanentLogin($create = false) { return ''; }
    }
    // Adminer calls this hook after opening its session. Seed automatic login
    // once, retaining its CSRF token and session ID on subsequent SQL requests.
    $driver = $GLOBALS['panelDriver'];
    $server = $GLOBALS['panelConnection']['server'];
    $user = $GLOBALS['panelConnection']['username'];
    if (($_SESSION['pwds'][$driver][$server][$user] ?? null) === null) {
        session_regenerate_id(true);
        Adminer\set_password($driver, $server, $user, 'panel-session');
        $_SESSION['db'][$driver][$server][$user][(string) $_GET['db']] = true;
    }
    return new PanelAdminer;
}
require '/var/www/html/adminer.php';
