package domain

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxRichTextBytes = 1 << 20
	maxRichTextNodes = 50_000
	maxRichTextDepth = 128
)

var (
	ErrInvalidRichText = errors.New("rich text must be valid UTF-8")
	ErrRichTextLimit   = errors.New("rich text exceeds sanitizer limits")
)

var allowedRichTextTags = map[string]bool{
	"a": true, "b": true, "blockquote": true, "br": true, "code": true,
	"em": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "hr": true, "i": true, "li": true,
	"ol": true, "p": true, "pre": true, "s": true, "span": true,
	"strong": true, "u": true, "ul": true,
}

var droppedRichTextTags = map[string]bool{
	"embed": true, "iframe": true, "math": true, "noscript": true,
	"object": true, "script": true, "style": true, "svg": true,
	"template": true,
}

// SanitizeRichText parses an HTML fragment and returns only allowlisted markup.
// Unknown formatting tags are unwrapped; active-content subtrees are removed.
func SanitizeRichText(value string) (string, error) {
	if len(value) > maxRichTextBytes {
		return "", ErrRichTextLimit
	}
	if !utf8.ValidString(value) {
		return "", ErrInvalidRichText
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(value), context)
	if err != nil {
		return "", fmt.Errorf("parse rich text: %w", err)
	}
	for _, node := range nodes {
		context.AppendChild(node)
	}
	budget := maxRichTextNodes
	if err := sanitizeRichTextChildren(context, 0, &budget); err != nil {
		return "", err
	}
	var output bytes.Buffer
	for node := context.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&output, node); err != nil {
			return "", fmt.Errorf("render sanitized rich text: %w", err)
		}
	}
	return output.String(), nil
}

func sanitizeRichTextChildren(parent *html.Node, depth int, budget *int) error {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		*budget--
		if *budget < 0 || depth > maxRichTextDepth {
			return ErrRichTextLimit
		}
		switch child.Type {
		case html.TextNode:
		case html.ElementNode:
			tag := strings.ToLower(child.Data)
			if droppedRichTextTags[tag] {
				parent.RemoveChild(child)
				child = next
				continue
			}
			sanitizeRichTextAttributes(child, tag)
			if err := sanitizeRichTextChildren(child, depth+1, budget); err != nil {
				return err
			}
			if !allowedRichTextTags[tag] {
				for nested := child.FirstChild; nested != nil; {
					nextNested := nested.NextSibling
					child.RemoveChild(nested)
					parent.InsertBefore(nested, child)
					nested = nextNested
				}
				parent.RemoveChild(child)
			}
		default:
			parent.RemoveChild(child)
		}
		child = next
	}
	return nil
}

func sanitizeRichTextAttributes(node *html.Node, tag string) {
	attributes := make([]html.Attribute, 0, len(node.Attr))
	blankTarget := false
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		value := strings.TrimSpace(attribute.Val)
		if len(value) > 4096 {
			continue
		}
		switch key {
		case "title":
			attributes = append(attributes, html.Attribute{Key: key, Val: value})
		case "lang":
			if value != "" {
				attributes = append(attributes, html.Attribute{Key: key, Val: value})
			}
		case "dir":
			direction := strings.ToLower(value)
			if direction == "ltr" || direction == "rtl" || direction == "auto" {
				attributes = append(attributes, html.Attribute{Key: key, Val: direction})
			}
		case "href":
			if tag == "a" && safeRichTextHref(value) {
				attributes = append(attributes, html.Attribute{Key: key, Val: value})
			}
		case "target":
			if tag == "a" && (value == "_blank" || value == "_self") {
				blankTarget = value == "_blank"
				attributes = append(attributes, html.Attribute{Key: key, Val: value})
			}
		}
	}
	if blankTarget {
		attributes = append(attributes, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
	}
	node.Attr = attributes
}

func safeRichTextHref(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "":
		return true
	case "http", "https", "mailto", "tel":
		return true
	default:
		return false
	}
}
