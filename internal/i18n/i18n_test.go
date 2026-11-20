package i18n

import (
	"net/http/httptest"
	"testing"
)

func TestTranslate(t *testing.T) {
	catalogs["xx"] = map[string]string{
		"not found":                            "introuvable",
		"Fetching branch %s":                   "Récupération de la branche %s",
		"push to %s; the app deploys %s":       "l’app déploie %[2]s, pas %[1]s",
		"Build variables: %d plain, %d secret": "Variables : %d simples, %d secrètes",
		"%d%% of %s":                           "%d %% de %s",
		"Deploying %s":                         "Déploiement de %s",
		"Deploying %s (image %s)":              "Déploiement de %s (image %s)",
		"untranslated":                         "",
	}
	defer func() { delete(catalogs, "xx"); delete(ready, "xx") }()
	for _, tc := range [][2]string{
		{"not found", "introuvable"},
		{"Fetching branch release/2.0", "Récupération de la branche release/2.0"},
		{"push to dev; the app deploys main", "l’app déploie main, pas dev"},
		{"Build variables: 3 plain, 1 secret", "Variables : 3 simples, 1 secrètes"},
		{"80% of disk", "80 % de disk"},
		{"Deploying web (image nginx:1.27)", "Déploiement de web (image nginx:1.27)"}, // the more specific pattern
		{"Deploying web", "Déploiement de web"},
		{"untranslated", "untranslated"},
		{"something else", "something else"},
		{"", ""},
	} {
		if got := Translate("xx", tc[0]); got != tc[1] {
			t.Errorf("Translate(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
	if got := Translate("en", "not found"); got != "not found" {
		t.Errorf("English = %q", got)
	}
	if got := Translate("zz", "not found"); got != "not found" {
		t.Errorf("unknown language = %q", got)
	}
}

func TestFromRequest(t *testing.T) {
	catalogs["xx"] = map[string]string{}
	defer delete(catalogs, "xx")
	for _, tc := range []struct{ cookie, header, want string }{
		{"", "", "en"}, {"xx", "", "xx"}, {"", "xx-CA,en;q=0.8", "xx"}, {"", "de, xx;q=0.5", "xx"},
		{"nope", "xx", "xx"}, {"en", "xx", "en"}, {"", "de", "en"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.cookie != "" {
			r.Header.Set("Cookie", Cookie+"="+tc.cookie)
		}
		r.Header.Set("Accept-Language", tc.header)
		if got := FromRequest(r); got != tc.want {
			t.Errorf("cookie %q, header %q = %q, want %q", tc.cookie, tc.header, got, tc.want)
		}
	}
}
