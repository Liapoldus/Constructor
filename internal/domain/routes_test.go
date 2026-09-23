package domain

import "testing"

func TestValidateRouteDocument(t *testing.T) {
	valid := []byte(`{"schemaVersion":1,"id":"site-routes","routes":[{"id":"home","path":"/","page":"Home","chunk":"lazy","lazy":true}]}`)
	if diagnostics, err := ValidateRouteDocument(valid, []string{"Home"}); err != nil || len(diagnostics) != 0 {
		t.Fatalf("valid route rejected: %#v %v", diagnostics, err)
	}
	invalid := []byte(`{"schemaVersion":1,"id":"site-routes","routes":[{"id":"same","path":"about","page":"Missing"},{"id":"same","path":"about","page":"Home"}]}`)
	if diagnostics, err := ValidateRouteDocument(invalid, []string{"Home"}); err != nil || len(diagnostics) < 4 {
		t.Fatalf("expected route diagnostics, got %#v %v", diagnostics, err)
	}
}

func TestValidateRoutePolicyAndLayoutReferences(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"site-routes","routes":[{"id":"home","path":"/","page":"Home","access":"Not Valid!","layouts":["shell","shell"]}]}`)
	diagnostics, err := ValidateRouteDocument(raw, []string{"Home"})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["route.access"] || !codes["route.layout-duplicate"] {
		t.Fatalf("expected invalid policy and duplicate layout diagnostics, got %#v", diagnostics)
	}
}

func TestValidateRouteDocumentReportsDuplicateRouteIdentities(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"id":"site-routes","routes":[{"id":"home","path":"/","page":"home"},{"id":"home","path":"/","page":"home"}]}`)
	diagnostics, err := ValidateRouteDocument(raw, []string{"home"})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["route.duplicate-id"] || !codes["route.duplicate-path"] {
		t.Fatalf("duplicate route IDs/paths were not rejected: %#v", diagnostics)
	}
}
