package domain

import "testing"

func TestDecodeThemeDocumentValidatesTypedTokensAndVariantOverrides(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"colors.primary":{"type":"color","value":"#1264a3"},"typography.body.font-size":{"type":"fontSize","value":"1rem"},"spacing.medium":{"type":"length","value":"16px"},"typography.body.font-weight":{"type":"fontWeight","value":400}},"variants":{"dark":{"colors.primary":"#b4d9fa"}}}`)
	theme, diagnostics := DecodeThemeDocument(raw, "liapoldus/themes/brand.json")
	if len(diagnostics) != 0 {
		t.Fatalf("valid theme rejected: %#v", diagnostics)
	}
	if theme.ID != "brand" || theme.Tokens["colors.primary"].Type != "color" || theme.Variants["dark"]["colors.primary"] != "#b4d9fa" {
		t.Fatalf("theme contract was not decoded: %#v", theme)
	}
}

func TestDecodeThemeDocumentRejectsUnsafeValuesAndUndeclaredVariantTokens(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"brand","name":"Brand","tokens":{"colors.primary":{"type":"color","value":"red; } body { display:none"},"spacing.small":{"type":"length","value":"url(javascript:alert(1))"}},"variants":{"dark":{"colors.missing":"#000000","spacing.small":"12px; color:red"}}}`)
	_, diagnostics := DecodeThemeDocument(raw, "liapoldus/themes/brand.json")
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	for _, expected := range []string{"theme.token-value", "theme.variant-token", "theme.variant-value"} {
		if !codes[expected] {
			t.Errorf("missing diagnostic %q: %#v", expected, diagnostics)
		}
	}
}
