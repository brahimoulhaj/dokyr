// Package i18n translates the messages the server writes for people: API errors and
// deployment log lines. The code keeps writing English; a catalog per language maps each
// English message to its translation, and a message is translated where it is shown, in
// the language of whoever asks (the stored log files and what API clients get by default
// stay English).
//
// A catalog key is the English text. A key with fmt verbs ("Fetching branch %s") is a
// pattern: it matches any text the format produces, and the translation is given the
// same values. A translation uses the verbs in the same order, or says which value it
// wants with an index ("%[2]s … %[1]s").
package i18n

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Default is the language the server writes in.
const Default = "en"

// catalogs holds the translations by language; filled by the messages_*.go files.
var catalogs = map[string]map[string]string{}

// Languages lists the languages with a catalog, and English.
func Languages() []string {
	out := []string{Default}
	for l := range catalogs {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Supported tells whether lang is English or has a catalog.
func Supported(lang string) bool {
	_, ok := catalogs[lang]
	return ok || lang == Default
}

var verb = regexp.MustCompile(`%(\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z]|%%`)

type pattern struct {
	prefix string // the literal text before the first verb: a cheap first test
	re     *regexp.Regexp
	to     string
}

type compiled struct {
	exact    map[string]string
	patterns []pattern
}

var (
	compileMu sync.Mutex
	ready     = map[string]*compiled{}
)

func catalog(lang string) *compiled {
	compileMu.Lock()
	defer compileMu.Unlock()
	if c, ok := ready[lang]; ok {
		return c
	}
	c := &compiled{exact: map[string]string{}}
	for from, to := range catalogs[lang] {
		if to == "" {
			continue
		}
		if !verb.MatchString(from) {
			c.exact[from] = to
			continue
		}
		var re strings.Builder
		re.WriteString("^")
		last, prefix, first := 0, "", true
		for _, m := range verb.FindAllStringIndex(from, -1) {
			lit := from[last:m[0]]
			if first {
				prefix, first = lit, false
			}
			re.WriteString(regexp.QuoteMeta(lit))
			if from[m[0]:m[1]] == "%%" {
				re.WriteString("%")
			} else {
				re.WriteString("(.*?)")
			}
			last = m[1]
		}
		re.WriteString(regexp.QuoteMeta(from[last:]))
		re.WriteString("$")
		c.patterns = append(c.patterns, pattern{prefix: strings.ReplaceAll(prefix, "%%", "%"), re: regexp.MustCompile("(?s)" + re.String()), to: to})
	}
	// The most literal text first, so a specific message wins over a loose one.
	sort.SliceStable(c.patterns, func(i, j int) bool {
		a, b := c.patterns[i], c.patterns[j]
		if la, lb := len(a.re.String()), len(b.re.String()); la != lb {
			return la > lb
		}
		return a.re.String() < b.re.String()
	})
	ready[lang] = c
	return c
}

// Translate returns text in lang, or text itself when lang is English, has no catalog,
// or the catalog does not know the message.
func Translate(lang, text string) string {
	if lang == Default || text == "" {
		return text
	}
	if _, ok := catalogs[lang]; !ok {
		return text
	}
	c := catalog(lang)
	if to, ok := c.match(text); ok {
		return to
	}
	// "what failed: why", as errors wrap each other: each side on its own.
	if head, tail, ok := strings.Cut(text, ": "); ok {
		h, hok := c.match(head)
		t, tok := c.match(tail)
		if hok || tok {
			if !hok {
				h = head
			}
			if !tok {
				t = tail
			}
			return h + ": " + t
		}
	}
	return text
}

func (c *compiled) match(text string) (string, bool) {
	if to, ok := c.exact[text]; ok {
		return to, true
	}
	for _, p := range c.patterns {
		if !strings.HasPrefix(text, p.prefix) {
			continue
		}
		if m := p.re.FindStringSubmatch(text); m != nil {
			return render(p.to, m[1:]), true
		}
	}
	return "", false
}

// render fills a translation's verbs with the values the pattern captured.
func render(to string, values []string) string {
	next := 0
	return verb.ReplaceAllStringFunc(to, func(v string) string {
		if v == "%%" {
			return "%"
		}
		i := next
		if m := verb.FindStringSubmatch(v); m[2] != "" {
			i = 0
			for _, d := range m[2] {
				i = i*10 + int(d-'0')
			}
			i--
		} else {
			next++
		}
		if i < 0 || i >= len(values) {
			return ""
		}
		return values[i]
	})
}

// Cookie is the cookie the web app keeps its language in, so that every request it
// makes — also the ones that cannot carry a header, like a log stream — says which.
const Cookie = "lang"

// FromRequest is the language a request asks for: the web app's cookie, else the first
// supported language of Accept-Language, else English.
func FromRequest(r *http.Request) string {
	if c, err := r.Cookie(Cookie); err == nil && Supported(c.Value) {
		return c.Value
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag, _, _ = strings.Cut(strings.ToLower(tag), "-")
		if Supported(tag) {
			return tag
		}
	}
	return Default
}
