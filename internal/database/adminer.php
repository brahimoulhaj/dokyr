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
// Scope PHP sessions to each database instance. Never forward
// the panel's own session cookie to PHP or place database passwords in the browser.
ini_set('session.use_strict_mode', '1');
session_name('panel_adminer');
session_set_cookie_params(['path' => $_SERVER['HTTP_X_PANEL_PATH'], 'httponly' => true, 'samesite' => 'Strict', 'secure' => ($_SERVER['HTTP_X_PANEL_HTTPS'] ?? '') === '1']);
session_start();
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
    // Seed the session on every request; SQL form CSRF tokens still use Adminer's session.
    if (session_status() !== PHP_SESSION_ACTIVE) session_start();
    Adminer\set_password($GLOBALS['panelDriver'], $GLOBALS['panelConnection']['server'], $GLOBALS['panelConnection']['username'], 'panel-session');
    return new PanelAdminer;
}
require '/var/www/html/adminer.php';
