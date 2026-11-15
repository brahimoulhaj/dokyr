package database

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise PHP, not just syntax: warnings become exceptions, and two requests
// share a session directory so a SQL form's CSRF token must survive navigation.
func TestAdminerSessionBootstrap(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP CLI is not installed")
	}
	for _, driver := range []string{"server", "pgsql"} {
		t.Run(driver, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "vendor.php")
			router := filepath.Join(dir, "router.php")
			harness := filepath.Join(dir, "request.php")
			// Session startup follows the pinned Adminer 5.5.1 bootstrap. Its own hook
			// is called only after the session and extension class have been initialized.
			vendor := `<?php
namespace Adminer {
 class Adminer {}
 function set_password($driver,$server,$user,$password) {
  $_SESSION['pwds'][$driver][$server][$user]=$password;
 }
}
namespace {
 if (session_status() !== PHP_SESSION_NONE) throw new \RuntimeException('Router started a session before Adminer');
 ini_set('session.use_trans_sid','0');
 if (!defined('SID')) {
  session_cache_limiter('');
  session_name('adminer_sid');
  session_set_cookie_params(0,preg_replace('~\?.*~','',$_SERVER['REQUEST_URI']),'',ini_get('session.cookie_secure')==='1',true);
  session_start();
 }
 $object=adminer_object();
 if ($object->credentials() !== ['database-host','app','test-database-secret']) throw new \RuntimeException('Wrong credentials');
 if (!$object->login('app','panel-session')) throw new \RuntimeException('Automatic login failed');
 if (($_SESSION['pwds'][$GLOBALS['panelDriver']]['database-host']['app'] ?? '') !== 'panel-session') throw new \RuntimeException('Login not seeded');
 if (empty($_SESSION['db'][$GLOBALS['panelDriver']]['database-host']['app']['app_db'])) throw new \RuntimeException('Database not seeded');
 if (isset($_COOKIE['adminer_sid']) && ($_SESSION['token'] ?? '') !== 'form-csrf-token') throw new \RuntimeException('SQL form session lost');
 $_SESSION['token']='form-csrf-token';
 $result=['id'=>session_id(),'name'=>session_name(),'path'=>session_get_cookie_params()['path'],'secure'=>session_get_cookie_params()['secure'],'same_site'=>session_get_cookie_params()['samesite']];
 session_write_close();
 header('X-Session-Test: ok');
 echo json_encode($result);
}
`
			if err := os.WriteFile(fixture, []byte(vendor), 0600); err != nil {
				t.Fatal(err)
			}
			quoted := func(s string) string {
				return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
			}
			wrapper := strings.Replace(adminerRouter, "require '/var/www/html/adminer.php';", "require "+quoted(fixture)+";", 1)
			if err := os.WriteFile(router, []byte(wrapper), 0600); err != nil {
				t.Fatal(err)
			}
			request := `<?php
error_reporting(E_ALL);
set_error_handler(function($severity,$message,$file,$line){throw new ErrorException($message,0,$severity,$file,$line);});
ini_set('session.save_path',__DIR__);
$_SERVER['HTTP_X_PANEL_GATEWAY']='test-gateway';
$_SERVER['HTTP_X_PANEL_CONNECTION']=base64_encode(json_encode(['driver'=>$argv[1],'server'=>'database-host','username'=>'app','password'=>'test-database-secret','database'=>'app_db']));
$_SERVER['HTTP_X_PANEL_PATH']='/api/databases/example/adminer/';
$_SERVER['HTTP_X_PANEL_HTTPS']='1';
$_SERVER['QUERY_STRING']='sql=';
$_GET=['sql'=>''];
if (!empty($argv[2])) $_COOKIE['adminer_sid']=$argv[2];
require ` + quoted(router) + ";\n"
			if err := os.WriteFile(harness, []byte(request), 0600); err != nil {
				t.Fatal(err)
			}
			run := func(cookie string) map[string]any {
				t.Helper()
				cmd := exec.Command(php, "-d", "display_errors=1", harness, driver, cookie)
				cmd.Env = append(os.Environ(), "PANEL_GATEWAY_KEY=test-gateway")
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("PHP session bootstrap: %v\n%s", err, output)
				}
				var result map[string]any
				if err = json.Unmarshal(output, &result); err != nil {
					t.Fatalf("PHP emitted warnings or invalid response: %s", output)
				}
				if result["name"] != "adminer_sid" || result["path"] != "/api/databases/example/adminer/" || result["secure"] != true || result["same_site"] != "Strict" {
					t.Fatalf("unsafe session: %+v", result)
				}
				return result
			}
			first := run("")
			second := run(first["id"].(string))
			if first["id"] != second["id"] {
				t.Fatal("existing session regenerated on SQL form request")
			}
		})
	}
}
