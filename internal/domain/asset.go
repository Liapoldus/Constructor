package domain

import (
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
)

var ErrAssetUploadInvalid = errors.New("asset upload is not a supported image")
var ErrAssetTooLarge = errors.New("asset upload exceeds the configured size limit")

type AssetDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	ID            string      `json:"id"`
	Items         []AssetItem `json:"items"`
}

type AssetItem struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Path     string         `json:"path"`
	MIMEType string         `json:"mimeType"`
	Size     int64          `json:"size"`
	SHA256   string         `json:"sha256"`
	Width    int            `json:"width,omitempty"`
	Height   int            `json:"height,omitempty"`
	Variants []AssetVariant `json:"variants,omitempty"`
}

type AssetVariant struct {
	Path     string `json:"path"`
	MIMEType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

var assetIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var assetMIMEPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
var assetSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var assetPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func DecodeAssetDocument(raw []byte, documentPath string) (AssetDocument, []Diagnostic) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var document AssetDocument
	if err := decoder.Decode(&document); err != nil {
		return AssetDocument{}, []Diagnostic{{Code: "asset.invalid-document", Severity: "error", Path: documentPath, Message: err.Error()}}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return AssetDocument{}, []Diagnostic{{Code: "asset.trailing-json", Severity: "error", Path: documentPath, Message: "document must contain exactly one JSON value"}}
	}
	var diagnostics []Diagnostic
	add := func(code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Severity: "error", Path: documentPath, Message: message})
	}
	if document.SchemaVersion != 1 {
		add("asset.schema-version", "schemaVersion must be 1")
	}
	if !assetIDPattern.MatchString(document.ID) {
		add("asset.document-id", "document id must be kebab-case")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, item := range document.Items {
		if !assetIDPattern.MatchString(item.ID) {
			add("asset.id", "asset ids must be kebab-case")
		}
		if ids[item.ID] {
			add("asset.duplicate-id", "asset ids must be unique")
		}
		ids[item.ID] = true
		switch item.Type {
		case "image", "icon", "font", "video", "document", "other":
		default:
			add("asset.type", "asset type must be image, icon, font, video, document, or other")
		}
		if !validAssetPath(item.Path) {
			add("asset.path", "asset path must be a normalized path inside public/assets")
		}
		if paths[item.Path] {
			add("asset.duplicate-path", "asset paths must be unique")
		}
		paths[item.Path] = true
		if !assetMIMEPattern.MatchString(item.MIMEType) {
			add("asset.mime-type", "mimeType must be a valid type/subtype")
		}
		if item.Size <= 0 {
			add("asset.size", "size must be greater than zero")
		}
		if !assetSHA256Pattern.MatchString(item.SHA256) {
			add("asset.sha256", "sha256 must be 64 lowercase hexadecimal characters")
		}
		if (item.Width == 0) != (item.Height == 0) || item.Width < 0 || item.Height < 0 {
			add("asset.dimensions", "width and height must both be positive or both omitted")
		}
		variantPaths := map[string]bool{}
		variantWidths := map[int]bool{}
		for _, variant := range item.Variants {
			if !validAssetPath(variant.Path) || paths[variant.Path] || variantPaths[variant.Path] {
				add("asset.variant-path", "variant paths must be unique normalized paths inside public/assets")
			}
			variantPaths[variant.Path] = true
			paths[variant.Path] = true
			if variantWidths[variant.Width] {
				add("asset.variant-width", "variant widths must be unique")
			}
			variantWidths[variant.Width] = true
			if item.Type != "image" || item.Width <= 0 || variant.MIMEType != "image/webp" || variant.Width <= 0 || variant.Height <= 0 || variant.Width >= item.Width || variant.Height > item.Height || variant.Size <= 0 || !assetSHA256Pattern.MatchString(variant.SHA256) {
				add("asset.variant-metadata", "variant must be a valid WebP image no wider than its source")
			}
		}
	}
	return document, diagnostics
}

func validAssetPath(value string) bool {
	if !strings.HasPrefix(value, "public/assets/") || strings.Contains(value, `\`) || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 3 || parts[0] != "public" || parts[1] != "assets" {
		return false
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." || !assetPathSegmentPattern.MatchString(part) {
			return false
		}
	}
	return true
}
