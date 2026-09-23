package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
	xwebp "golang.org/x/image/webp"
)

const MaxAssetUploadBytes int64 = 25 << 20

var responsiveImageWidths = [...]int{320, 640, 1280, 1920}

const responsiveImageQuality = 82

type AssetUploadResult struct {
	Asset  domain.AssetItem `json:"asset"`
	Reused bool             `json:"reused"`
}

type detectedAsset struct {
	mime     string
	ext      string
	typeName string
}

func (s *ProjectService) UploadAsset(upload []byte) (AssetUploadResult, error) {
	if len(upload) == 0 {
		return AssetUploadResult{}, domain.ErrAssetUploadInvalid
	}
	if int64(len(upload)) > MaxAssetUploadBytes {
		return AssetUploadResult{}, domain.ErrAssetTooLarge
	}
	// Keep peak decode/encode memory bounded when a local API client uploads
	// several large assets concurrently.
	s.assetProcessing.Lock()
	defer s.assetProcessing.Unlock()
	content, kind, err := validateAndNormalizeAsset(upload)
	if err != nil {
		return AssetUploadResult{}, err
	}
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	variants, width, height, err := createResponsiveVariants(content, kind, sha)
	if err != nil {
		return AssetUploadResult{}, err
	}

	s.mutation.Lock()
	defer s.mutation.Unlock()
	writer, ok := s.repository.(domain.ProjectFileBatchWriter)
	if !ok {
		return AssetUploadResult{}, domain.ErrUnsupported
	}
	document, exists, diagnostics, err := s.readAssetDocument()
	if err != nil {
		return AssetUploadResult{}, err
	}
	if hasErrors(diagnostics) {
		return AssetUploadResult{}, domain.StructuredValidationError{Diagnostics: diagnostics}
	}
	if !exists {
		document = domain.AssetDocument{SchemaVersion: 1, ID: "assets", Items: []domain.AssetItem{}}
	}
	for _, item := range document.Items {
		if item.SHA256 == sha {
			return AssetUploadResult{Asset: item, Reused: true}, nil
		}
	}

	assetID := "asset-" + sha[:24]
	for _, item := range document.Items {
		if item.ID == assetID && item.SHA256 != sha {
			assetID = "asset-" + sha[:48]
			break
		}
	}
	for _, item := range document.Items {
		if item.ID == assetID && item.SHA256 != sha {
			return AssetUploadResult{}, domain.ErrConflict
		}
	}
	assetPath := path.Join("public", "assets", assetID+"."+kind.ext)
	asset := domain.AssetItem{ID: assetID, Type: kind.typeName, Path: assetPath, MIMEType: kind.mime, Size: int64(len(content)), SHA256: sha, Width: width, Height: height}
	changes := []domain.ProjectFileChange{{Path: assetPath, Content: content}}
	for _, variant := range variants {
		asset.Variants = append(asset.Variants, variant.metadata)
		changes = append(changes, domain.ProjectFileChange{Path: variant.metadata.Path, Content: variant.content})
	}
	document.Items = append(document.Items, asset)
	catalog, err := jsonMarshalAssetDocument(document)
	if err != nil {
		return AssetUploadResult{}, err
	}
	catalogRevision := ""
	if exists {
		file, readErr := s.repository.Read(assetCatalogPath)
		if readErr != nil {
			return AssetUploadResult{}, readErr
		}
		catalogRevision = file.Revision
	}
	changes = append(changes, domain.ProjectFileChange{Path: assetCatalogPath, ExpectedRevision: catalogRevision, Content: catalog})
	if err := writer.ApplyBatch(changes); err != nil {
		return AssetUploadResult{}, err
	}
	return AssetUploadResult{Asset: asset}, nil
}

type generatedImageVariant struct {
	metadata domain.AssetVariant
	content  []byte
}

func createResponsiveVariants(content []byte, kind detectedAsset, sourceSHA string) ([]generatedImageVariant, int, int, error) {
	if kind.typeName != "image" {
		return nil, 0, 0, nil
	}
	if kind.ext == "gif" {
		config, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil {
			return nil, 0, 0, domain.ErrAssetUploadInvalid
		}
		return nil, config.Width, config.Height, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, 0, 0, domain.ErrAssetUploadInvalid
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	variants := make([]generatedImageVariant, 0, len(responsiveImageWidths))
	for _, targetWidth := range responsiveImageWidths {
		if targetWidth >= width { // no upscaling; original remains the fallback
			continue
		}
		targetHeight := int(math.Round(float64(height) * float64(targetWidth) / float64(width)))
		if targetHeight < 1 {
			targetHeight = 1
		}
		resized := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), decoded, bounds, draw.Over, nil)
		var encoded bytes.Buffer
		if err := webp.Encode(&encoded, resized, webp.Options{Quality: responsiveImageQuality, Method: 4}); err != nil {
			return nil, 0, 0, fmt.Errorf("encode WebP variant at %dpx: %w", targetWidth, err)
		}
		variantContent := encoded.Bytes()
		digest := sha256.Sum256(variantContent)
		variantPath := path.Join("public", "assets", fmt.Sprintf("asset-%s-%d.webp", sourceSHA, targetWidth))
		variants = append(variants, generatedImageVariant{
			metadata: domain.AssetVariant{Path: variantPath, MIMEType: "image/webp", Width: targetWidth, Height: targetHeight, Size: int64(len(variantContent)), SHA256: hex.EncodeToString(digest[:])},
			content:  bytes.Clone(variantContent),
		})
	}
	return variants, width, height, nil
}

func jsonMarshalAssetDocument(document domain.AssetDocument) ([]byte, error) {
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func validateAndNormalizeAsset(upload []byte) ([]byte, detectedAsset, error) {
	if looksLikeSVG(upload) {
		sanitized, err := sanitizeSVG(upload)
		if err != nil {
			return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
		}
		if int64(len(sanitized)) > MaxAssetUploadBytes {
			return nil, detectedAsset{}, domain.ErrAssetTooLarge
		}
		return sanitized, detectedAsset{mime: "image/svg+xml", ext: "svg", typeName: "icon"}, nil
	}

	mimeType := strings.Split(http.DetectContentType(upload), ";")[0]
	var expectedFormat string
	switch mimeType {
	case "image/png":
		expectedFormat = "png"
	case "image/jpeg":
		expectedFormat = "jpeg"
	case "image/gif":
		expectedFormat = "gif"
	case "image/webp":
		config, err := xwebp.DecodeConfig(bytes.NewReader(upload))
		if err != nil || !validImageDimensions(config.Width, config.Height) {
			return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
		}
		if _, err := xwebp.Decode(bytes.NewReader(upload)); err != nil {
			return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
		}
		return bytes.Clone(upload), detectedAsset{mime: "image/webp", ext: "webp", typeName: "image"}, nil
	default:
		return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(upload))
	if err != nil || format != expectedFormat || !validImageDimensions(config.Width, config.Height) {
		return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
	}
	if _, decodedFormat, decodeErr := image.Decode(bytes.NewReader(upload)); decodeErr != nil || decodedFormat != expectedFormat {
		return nil, detectedAsset{}, domain.ErrAssetUploadInvalid
	}
	extension := expectedFormat
	if extension == "jpeg" {
		extension = "jpg"
	}
	return bytes.Clone(upload), detectedAsset{mime: mimeType, ext: extension, typeName: "image"}, nil
}

func validImageDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= 50000 && height <= 50000 && int64(width)*int64(height) <= 25_000_000
}

func looksLikeSVG(content []byte) bool {
	trimmed := bytes.TrimSpace(content)
	return bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml"))
}

var svgElementAllowlist = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true, "circle": true,
	"ellipse": true, "line": true, "polyline": true, "polygon": true,
	"defs": true, "linearGradient": true, "radialGradient": true, "stop": true,
	"clipPath": true, "mask": true, "title": true, "desc": true,
}

var svgAttributeAllowlist = map[string]bool{
	"xmlns": true, "id": true, "viewBox": true, "width": true, "height": true,
	"x": true, "y": true, "x1": true, "x2": true, "y1": true, "y2": true,
	"cx": true, "cy": true, "r": true, "rx": true, "ry": true,
	"d": true, "points": true, "fill": true, "fill-rule": true,
	"clip-rule": true, "stroke": true, "stroke-width": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-dasharray": true, "stroke-dashoffset": true,
	"opacity": true, "fill-opacity": true, "stroke-opacity": true,
	"transform": true, "offset": true, "stop-color": true, "stop-opacity": true,
	"gradientUnits": true, "gradientTransform": true, "spreadMethod": true,
	"clipPathUnits": true, "maskUnits": true, "maskContentUnits": true,
	"preserveAspectRatio": true, "href": true,
}

var svgIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$`)
var svgNumberPattern = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?(?:px|pt|pc|mm|cm|in|%)?$`)
var svgPathPattern = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9eE+.,\-\s]*$`)
var svgTransformPattern = regexp.MustCompile(`^[A-Za-z0-9eE+.,()\-\s]+$`)
var svgPaintPattern = regexp.MustCompile(`^(?:none|currentColor|transparent|#[A-Fa-f0-9]{3,8}|[A-Za-z]{1,24}|url\(#[A-Za-z_][A-Za-z0-9_.:-]{0,127}\))$`)

func sanitizeSVG(content []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.Strict = true
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	depth, suppressed := 0, 0
	rootSeen, rootClosed := false, false
	tokens := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		tokens++
		if tokens > 200_000 {
			return nil, errors.New("SVG has too many XML tokens")
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, errors.New("SVG directives are not allowed")
		case xml.StartElement:
			depth++
			if depth > 128 {
				return nil, errors.New("SVG nesting is too deep")
			}
			if depth == 1 && rootClosed {
				return nil, errors.New("SVG must contain one root element")
			}
			if suppressed > 0 {
				suppressed++
				continue
			}
			if !svgElementAllowlist[value.Name.Local] {
				suppressed = 1
				continue
			}
			if !rootSeen {
				if value.Name.Local != "svg" {
					return nil, errors.New("SVG root element is required")
				}
				rootSeen = true
			}
			start := xml.StartElement{Name: xml.Name{Local: value.Name.Local}}
			for _, attr := range value.Attr {
				name := attr.Name.Local
				if name == "xmlns" {
					if attr.Value == "http://www.w3.org/2000/svg" {
						start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: attr.Value})
					}
					continue
				}
				if !svgAttributeAllowlist[name] || !safeSVGAttribute(name, attr.Value) {
					continue
				}
				start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: name}, Value: attr.Value})
			}
			if err := encoder.EncodeToken(start); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if suppressed > 0 {
				suppressed--
				depth--
				continue
			}
			if rootSeen {
				if err := encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: value.Name.Local}}); err != nil {
					return nil, err
				}
			}
			depth--
			if depth == 0 && rootSeen {
				rootClosed = true
			}
		case xml.CharData:
			if suppressed == 0 && rootSeen {
				if err := encoder.EncodeToken(xml.CharData(bytes.Clone(value))); err != nil {
					return nil, err
				}
			}
		case xml.ProcInst:
			if value.Target != "xml" || rootSeen {
				return nil, errors.New("SVG processing instructions are not allowed")
			}
		}
	}
	if !rootSeen || !rootClosed || depth != 0 || suppressed != 0 {
		return nil, fmt.Errorf("SVG document is incomplete")
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func safeSVGAttribute(name, value string) bool {
	if strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	switch name {
	case "id":
		return svgIDPattern.MatchString(value)
	case "xmlns":
		return value == "http://www.w3.org/2000/svg"
	case "href":
		return strings.HasPrefix(value, "#") && svgIDPattern.MatchString(strings.TrimPrefix(value, "#"))
	case "fill", "stroke", "stop-color":
		return svgPaintPattern.MatchString(value)
	case "d", "points":
		return svgPathPattern.MatchString(value)
	case "transform", "gradientTransform":
		return svgTransformPattern.MatchString(value)
	case "width", "height", "x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "stroke-width", "stroke-dashoffset", "stroke-dasharray", "opacity", "fill-opacity", "stroke-opacity", "offset", "stop-opacity":
		for _, component := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if component != "none" && !svgNumberPattern.MatchString(component) {
				return false
			}
		}
		return value != ""
	case "viewBox":
		parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
		if len(parts) != 4 {
			return false
		}
		for _, part := range parts {
			if !regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`).MatchString(part) {
				return false
			}
		}
		return true
	case "fill-rule", "clip-rule":
		return value == "nonzero" || value == "evenodd"
	case "stroke-linecap":
		return value == "butt" || value == "round" || value == "square"
	case "stroke-linejoin":
		return value == "miter" || value == "round" || value == "bevel"
	case "gradientUnits", "clipPathUnits", "maskUnits", "maskContentUnits":
		return value == "userSpaceOnUse" || value == "objectBoundingBox"
	case "spreadMethod":
		return value == "pad" || value == "reflect" || value == "repeat"
	case "preserveAspectRatio":
		return regexp.MustCompile(`^(?:none|x(?:Min|Mid|Max)Y(?:Min|Mid|Max)(?: meet| slice)?)$`).MatchString(value)
	default:
		return false
	}
}
