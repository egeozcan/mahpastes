package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"go-clipboard/internal/openrequest"
)

// The macOS bundle identity lives in build/darwin/Info.plist and
// Info.dev.plist, Wails templates that only a macOS build renders. These tests
// render them the way Wails does and check what the rest of the repo relies
// on, so a mistake shows up on any OS.

// wailsAssetData mirrors the data Wails v2 executes build asset templates
// with (pkg/buildassets assetData).
type wailsAssetData struct {
	Name           string
	OutputFilename string
	Info           struct {
		CompanyName    string
		ProductName    string
		ProductVersion string
		Copyright      *string
		Comments       *string
	}
}

type wailsProject struct {
	Name           string `json:"name"`
	OutputFilename string `json:"outputfilename"`
	Info           struct {
		CompanyName    string  `json:"companyName"`
		ProductName    string  `json:"productName"`
		ProductVersion string  `json:"productVersion"`
		Copyright      *string `json:"copyright"`
		Comments       *string `json:"comments"`
	} `json:"info"`
}

func readWailsProject(t *testing.T) wailsProject {
	t.Helper()
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var p wailsProject
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("wails.json: %v", err)
	}
	return p
}

// renderPlist renders a committed plist template with wails.json's values and
// decodes the result.
func renderPlist(t *testing.T, path string) map[string]any {
	t.Helper()
	p := readWailsProject(t)
	var data wailsAssetData
	data.Name = p.Name
	data.OutputFilename = p.OutputFilename
	data.Info.CompanyName = p.Info.CompanyName
	data.Info.ProductName = p.Info.ProductName
	data.Info.ProductVersion = p.Info.ProductVersion
	data.Info.Copyright = p.Info.Copyright
	data.Info.Comments = p.Info.Comments

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New(path).Parse(string(src))
	if err != nil {
		t.Fatalf("%s is not a valid Wails template: %v", path, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatalf("render %s: %v", path, err)
	}
	dict, err := decodePlist(out.Bytes())
	if err != nil {
		t.Fatalf("%s does not render to a valid plist: %v\n%s", path, err, out.String())
	}
	return dict
}

// decodePlist decodes an XML property list whose root is a dict.
func decodePlist(b []byte) (map[string]any, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dict" {
			v, err := decodePlistValue(d, se)
			if err != nil {
				return nil, err
			}
			return v.(map[string]any), nil
		}
	}
}

func decodePlistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		out := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if el.Name.Local == "key" {
					var k string
					if err := d.DecodeElement(&k, &el); err != nil {
						return nil, err
					}
					if _, dup := out[k]; dup {
						return nil, fmt.Errorf("duplicate key %q", k)
					}
					key = k
					continue
				}
				if key == "" {
					return nil, fmt.Errorf("<%s> without a key", el.Name.Local)
				}
				v, err := decodePlistValue(d, el)
				if err != nil {
					return nil, err
				}
				out[key] = v
				key = ""
			case xml.EndElement:
				if key != "" {
					return nil, fmt.Errorf("key %q has no value", key)
				}
				return out, nil
			}
		}
	case "array":
		out := []any{}
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				v, err := decodePlistValue(d, el)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			case xml.EndElement:
				return out, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return start.Name.Local == "true", nil
	case "string", "integer", "real", "date", "data":
		var s string
		if err := d.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, fmt.Errorf("unexpected <%s>", start.Name.Local)
}

// fileProviderBundleID is the identifier scripts/macos/build-file-provider.sh
// signs and entitles the app under by default.
func fileProviderBundleID(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("scripts/macos/build-file-provider.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`FP_BUNDLE_ID="\$\{FP_BUNDLE_ID:-([^}]+)\}"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("FP_BUNDLE_ID default not found in build-file-provider.sh")
	}
	return string(m[1])
}

func TestInfoPlistIdentityMatchesSigning(t *testing.T) {
	want := fileProviderBundleID(t)
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		p := renderPlist(t, path)
		if got := p["CFBundleIdentifier"]; got != want {
			t.Errorf("%s: CFBundleIdentifier = %v, want %q (the File Provider signing identity)", path, got, want)
		}
		if got := p["CFBundleExecutable"]; got != readWailsProject(t).OutputFilename {
			t.Errorf("%s: CFBundleExecutable = %v, want wails.json outputfilename", path, got)
		}
	}
}

func TestInfoPlistVersionComesFromWailsJSON(t *testing.T) {
	version := readWailsProject(t).Info.ProductVersion
	// CFBundleShortVersionString and the Windows file version both need plain
	// dotted numbers; scripts/version.sh --stamp writes only that form.
	if !regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`).MatchString(version) {
		t.Fatalf("wails.json info.productVersion %q is not a dotted number", version)
	}
	p := renderPlist(t, "build/darwin/Info.plist")
	for _, key := range []string{"CFBundleShortVersionString", "CFBundleVersion"} {
		if p[key] != version {
			t.Errorf("%s = %v, want %q", key, p[key], version)
		}
	}
	if s, _ := p["NSHumanReadableCopyright"].(string); s == "" || strings.Contains(s, "....") {
		t.Errorf("NSHumanReadableCopyright = %q, want the wails.json copyright", s)
	}
}

func TestInfoPlistRegistersURLSchemeAndDocuments(t *testing.T) {
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		p := renderPlist(t, path)

		urlTypes, _ := p["CFBundleURLTypes"].([]any)
		found := false
		for _, ut := range urlTypes {
			d, _ := ut.(map[string]any)
			for _, s := range asSlice(d["CFBundleURLSchemes"]) {
				if s == openrequest.URLScheme {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: no CFBundleURLTypes entry for %q", path, openrequest.URLScheme)
		}

		docTypes, _ := p["CFBundleDocumentTypes"].([]any)
		if len(docTypes) != 1 {
			t.Fatalf("%s: CFBundleDocumentTypes = %v, want one catch-all entry", path, docTypes)
		}
		doc, _ := docTypes[0].(map[string]any)
		// Alternate: listed under Open With, never made the default handler.
		if doc["LSHandlerRank"] != "Alternate" || doc["CFBundleTypeRole"] != "Viewer" {
			t.Errorf("%s: document type %v must be a Viewer with rank Alternate", path, doc)
		}
		if !reflect.DeepEqual(asSlice(doc["LSItemContentTypes"]), []any{"public.data", "public.content"}) {
			t.Errorf("%s: LSItemContentTypes = %v", path, doc["LSItemContentTypes"])
		}

		for _, key := range []string{
			"NSLocalNetworkUsageDescription",
			"NSDesktopFolderUsageDescription",
			"NSDownloadsFolderUsageDescription",
			"NSDocumentsFolderUsageDescription",
		} {
			if s, _ := p[key].(string); len(s) < 20 {
				t.Errorf("%s: %s = %q, want a usage description", path, key, s)
			}
		}
	}
}

// The dev plist is the release plist plus App Transport Security for the
// wails dev server; anything else drifting between them is a mistake.
func TestInfoDevPlistOnlyAddsTransportSecurity(t *testing.T) {
	prod := renderPlist(t, "build/darwin/Info.plist")
	dev := renderPlist(t, "build/darwin/Info.dev.plist")
	if _, ok := dev["NSAppTransportSecurity"]; !ok {
		t.Error("Info.dev.plist lost NSAppTransportSecurity, which wails dev needs")
	}
	if _, ok := prod["NSAppTransportSecurity"]; ok {
		t.Error("Info.plist must not relax App Transport Security")
	}
	delete(dev, "NSAppTransportSecurity")
	if !reflect.DeepEqual(prod, dev) {
		t.Errorf("Info.plist and Info.dev.plist differ:\nprod %v\ndev  %v", prod, dev)
	}
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
