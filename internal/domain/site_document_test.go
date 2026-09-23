package domain

import "testing"

func TestDecodeSiteDocumentUsesStablePageIDsAndDisplayNames(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","pages":[{"id":"home","name":"Главная"},{"id":"about","name":"Страница"},{"id":"contact","name":"Страница"}],"locales":["ru-RU"]}`)
	site, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	if len(diagnostics) != 0 {
		t.Fatalf("valid Site page objects rejected: %#v", diagnostics)
	}
	if len(site.Pages) != 3 || site.Pages[0].ID != "home" || site.Pages[0].Name != "Главная" || site.Pages[2].ID != "contact" {
		t.Fatalf("Site page identity or display name was not preserved: %#v", site.Pages)
	}
}

func TestDecodeSiteDocumentReadsLegacyStringPageIDs(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","pages":[ "home" ],"locales":["ru-RU"]}`)
	site, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	if len(diagnostics) != 0 || len(site.Pages) != 1 {
		t.Fatalf("legacy string Site page rejected: site=%#v diagnostics=%#v", site, diagnostics)
	}
	if site.Pages[0].ID != "home" || site.Pages[0].Name != "home" {
		t.Fatalf("legacy page should normalize to ID and same-name label: %#v", site.Pages[0])
	}
}

func TestDecodeSiteDocumentAcceptsAndValidatesThemeID(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","themeId":"brand","pages":["home"],"locales":["ru-RU"]}`)
	site, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	if len(diagnostics) != 0 || site.ThemeID != "brand" {
		t.Fatalf("valid Site theme selection rejected: %#v %#v", site, diagnostics)
	}
	raw = []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","themeId":"../brand","pages":["home"],"locales":["ru-RU"]}`)
	_, diagnostics = DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	if len(diagnostics) == 0 || diagnostics[0].Code != "site.theme-id" {
		t.Fatalf("path-unsafe theme ID was accepted: %#v", diagnostics)
	}
}

func TestDecodeSiteDocumentRejectsMalformedPageObjects(t *testing.T) {
	for _, page := range []string{`{"id":"Home","name":"Home"}`, `{"id":"home","name":" "}`, `{"id":"home","name":"Home","unexpected":true}`} {
		raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","pages":[` + page + `],"locales":["ru-RU"]}`)
		_, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
		if len(diagnostics) == 0 {
			t.Fatalf("malformed Site page accepted: %s", page)
		}
	}
}

func TestDecodeSiteDocumentRejectsDuplicatePageAndLocaleIDs(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","pages":["home","home"],"locales":["en","en"]}`)
	_, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["site.page-duplicate"] || !codes["site.locale-duplicate"] {
		t.Fatalf("duplicate Site identities were not rejected: %#v", diagnostics)
	}
}

func TestDecodeSiteDocumentRequiresAtLeastOneEnabledLocale(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"demo-site","projectId":"demo","name":"Demo","pages":["home"],"locales":[]}`)
	_, diagnostics := DecodeSiteDocument(raw, "liapoldus/sites/demo-site.json")
	if len(diagnostics) != 1 || diagnostics[0].Code != "site.locales-required" {
		t.Fatalf("empty enabled-locale list was accepted: %#v", diagnostics)
	}
}
