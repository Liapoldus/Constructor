package domain

import (
	"strings"
	"testing"
)

func TestSanitizeRichTextRemovesActiveMarkupAndPreservesSafeFormatting(t *testing.T) {
	input := `<p onclick="alert(1)">Hello<script>alert(1)</script><a href="javascript:alert(1)" onclick="alert(2)">unsafe link</a><svg><script>alert(3)</script></svg><custom><script>alert(4)</script><strong style="color:red">safe</strong></custom></p><a href="https://example.test/path" target="_blank">safe link</a>`
	sanitized, err := SanitizeRichText(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<script", "<svg", "onclick", "style=", "javascript:"} {
		if strings.Contains(strings.ToLower(sanitized), forbidden) {
			t.Fatalf("sanitized output contains %q: %s", forbidden, sanitized)
		}
	}
	for _, safe := range []string{"<p>Hello", "<a>unsafe link</a>", "<strong>safe</strong>", `href="https://example.test/path"`, `rel="noopener noreferrer"`} {
		if !strings.Contains(sanitized, safe) {
			t.Fatalf("sanitized output is missing %q: %s", safe, sanitized)
		}
	}
}

func TestSanitizeRichTextRejectsOversizedAndInvalidUTF8(t *testing.T) {
	if _, err := SanitizeRichText(strings.Repeat("x", maxRichTextBytes+1)); err != ErrRichTextLimit {
		t.Fatalf("oversized input error = %v, want ErrRichTextLimit", err)
	}
	if _, err := SanitizeRichText(string([]byte{0xff})); err != ErrInvalidRichText {
		t.Fatalf("invalid UTF-8 error = %v, want ErrInvalidRichText", err)
	}
	deepMarkup := strings.Repeat("<b>", maxRichTextDepth+1) + "text" + strings.Repeat("</b>", maxRichTextDepth+1)
	if _, err := SanitizeRichText(deepMarkup); err != ErrRichTextLimit {
		t.Fatalf("deep markup error = %v, want ErrRichTextLimit", err)
	}
}
