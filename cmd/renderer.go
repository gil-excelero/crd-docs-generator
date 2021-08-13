package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	"k8s.io/gengo/types"
	"k8s.io/klog"

	texttemplate "text/template"
)

func Render(w io.Writer, pkgs []*apiPackage, config GeneratorConfig) error {
	references := findTypeReferences(pkgs)
	typePkgMap := extractTypeToPackageMap(pkgs)

	t, err := template.New("").Funcs(map[string]interface{}{
		"isExportedType":     isExportedType,
		"fieldName":          fieldName,
		"fieldEmbedded":      fieldEmbedded,
		"typeIdentifier":     func(t *types.Type) string { return typeIdentifier(t) },
		"typeDisplayName":    func(t *types.Type) string { return typeDisplayName(t, config, typePkgMap) },
		"visibleTypes":       func(t []*types.Type) []*types.Type { return visibleTypes(t, config) },
		"renderComments":     func(s []string) string { return renderComments(s, !config.MarkdownDisabled) },
		"packageDisplayName": func(p *apiPackage) string { return p.identifier() },
		"apiGroup":           func(t *types.Type) string { return apiGroupForType(t, typePkgMap) },
		"packageAnchorID": func(p *apiPackage) string {
			// space trimmed displayName
			return strings.Replace(p.identifier(), " ", "", -1)
		},
		"linkForType": func(t *types.Type) string {
			v, err := linkForType(t, config, typePkgMap)
			if err != nil {
				klog.Fatal(errors.Wrapf(err, "error getting link for type=%s", t.Name))
				return ""
			}
			return v
		},
		"asciidocLinkForType": func(t *types.Type) string {
			link, err := linkForType(t, config, typePkgMap)
			if err != nil {
				klog.Fatal(errors.Wrapf(err, "error getting link for type=%s", t.Name))
				return ""
			}

			displayName := typeDisplayName(t, config, typePkgMap)

			if strings.HasPrefix(link, "#") {
				return fmt.Sprintf("xref:%s[$$%s$$]", strings.TrimPrefix(link, "#"), displayName)
			}
			return fmt.Sprintf("link:%s[$$%s$$]", link, displayName)
		},
		"anchorIDForType":  func(t *types.Type) string { return anchorIDForLocalType(t, typePkgMap) },
		"safe":             safe,
		"sortedTypes":      sortTypes,
		"typeReferences":   func(t *types.Type) []*types.Type { return typeReferences(t, config, references) },
		"hiddenMember":     func(m types.Member) bool { return hiddenMember(m, config) },
		"isLocalType":      isLocalType,
		"isOptionalMember": isOptionalMember,
		"safeIdentifier":   safeIdentifier,
		"constantsOfType":  func(t *types.Type) []*types.Type { return constantsOfType(t, typePkgMap[t]) },
	}).ParseGlob(filepath.Join(*flTemplateDir, "*.tpl"))
	if err != nil {
		return errors.Wrap(err, "parse error")
	}

	var gitCommit []byte
	if !config.GitCommitDisabled {
		gitCommit, _ = exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	}

	return errors.Wrap(t.ExecuteTemplate(w, "page", map[string]interface{}{
		"packages":  pkgs,
		"config":    config,
		"gitCommit": strings.TrimSpace(string(gitCommit)),
	}), "template execution error")
}

// anchorIDForLocalType returns the #anchor string for the local type
func anchorIDForLocalType(t *types.Type, typePkgMap map[*types.Type]*apiPackage) string {
	return safeIdentifier(fmt.Sprintf("%s.%s", apiGroupForType(t, typePkgMap), t.Name.Name))
}

func safeIdentifier(id string) string {
	return strings.ToLower(safeIDRegex.ReplaceAllLiteralString(id, "-"))
}

// extractTypeToPackageMap creates a *types.Type map to apiPackage
func extractTypeToPackageMap(pkgs []*apiPackage) map[*types.Type]*apiPackage {
	out := make(map[*types.Type]*apiPackage)
	for _, ap := range pkgs {
		for _, t := range ap.Types {
			out[t] = ap
		}
		for _, t := range ap.Constants {
			out[t] = ap
		}
	}
	return out
}

func findTypeReferences(pkgs []*apiPackage) map[*types.Type][]*types.Type {
	m := make(map[*types.Type][]*types.Type)
	for _, pkg := range pkgs {
		for _, typ := range pkg.Types {
			for _, member := range typ.Members {
				t := member.Type
				t = tryDereference(t)
				m[t] = append(m[t], typ)
			}
		}
	}
	return m
}

// linkForType returns an anchor to the type if it can be generated. returns
// empty string if it is not a local type or unrecognized external type.
func linkForType(t *types.Type, c GeneratorConfig, typePkgMap map[*types.Type]*apiPackage) (string, error) {
	t = tryDereference(t) // dereference kind=Pointer

	if isLocalType(t, typePkgMap) {
		return "#" + anchorIDForLocalType(t, typePkgMap), nil
	}

	var arrIndex = func(a []string, i int) string {
		return a[(len(a)+i)%len(a)]
	}

	// types like k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta,
	// k8s.io/api/core/v1.Container, k8s.io/api/autoscaling/v1.CrossVersionObjectReference,
	// github.com/knative/build/pkg/apis/build/v1alpha1.BuildSpec
	if t.Kind == types.Struct || t.Kind == types.Pointer || t.Kind == types.Interface || t.Kind == types.Alias {
		id := typeIdentifier(t)                        // gives {{ImportPath.Identifier}} for type
		segments := strings.Split(t.Name.Package, "/") // to parse [meta, v1] from "k8s.io/apimachinery/pkg/apis/meta/v1"

		for _, v := range c.ExternalPackages {
			r, err := regexp.Compile(v.TypeMatchPrefix)
			if err != nil {
				return "", errors.Wrapf(err, "pattern %q failed to compile", v.TypeMatchPrefix)
			}
			if r.MatchString(id) {
				tpl, err := texttemplate.New("").Funcs(map[string]interface{}{
					"lower":    strings.ToLower,
					"arrIndex": arrIndex,
				}).Parse(v.DocsURLTemplate)
				if err != nil {
					return "", errors.Wrap(err, "docs URL template failed to parse")
				}

				var b bytes.Buffer
				if err := tpl.
					Execute(&b, map[string]interface{}{
						"TypeIdentifier":  t.Name.Name,
						"PackagePath":     t.Name.Package,
						"PackageSegments": segments,
					}); err != nil {
					return "", errors.Wrap(err, "docs url template execution error")
				}
				return b.String(), nil
			}
		}
		klog.Warningf("not found external link source for type %v", t.Name)
	}
	return "", nil
}

func typeDisplayName(t *types.Type, c GeneratorConfig, typePkgMap map[*types.Type]*apiPackage) string {
	s := typeIdentifier(t)

	if isLocalType(t, typePkgMap) {
		s = tryDereference(t).Name.Name
	}

	if t.Kind == types.Pointer {
		s = strings.TrimLeft(s, "*")
	}

	switch t.Kind {
	case types.Struct,
		types.Interface,
		types.Alias,
		types.Pointer,
		types.Slice,
		types.Builtin:
		// noop
	case types.Unsupported:
		return ""
	case types.Map:
		// return original name
		return t.Name.Name
	case types.DeclarationOf:
		// For constants, we want to display the value
		// rather than the name of the constant, since the
		// value is what users will need to write into YAML
		// specs.
		if t.ConstValue != nil {
			u := finalUnderlyingTypeOf(t)
			// Quote string constants to make it clear to the documentation reader.
			if u.Kind == types.Builtin && u.Name.Name == "string" {
				return strconv.Quote(*t.ConstValue)
			}

			return *t.ConstValue
		}
		klog.Fatalf("type %s is a non-const declaration, which is unhandled", t.Name)
	default:
		klog.Fatalf("type %s has kind=%v which is unhandled", t.Name, t.Kind)
	}

	// substitute prefix, if registered
	for prefix, replacement := range c.TypeDisplayNamePrefixOverrides {
		if strings.HasPrefix(s, prefix) {
			s = strings.Replace(s, prefix, replacement, 1)
		}
	}

	if t.Kind == types.Slice {
		s = "[]" + s
	}

	return s
}

func hideType(t *types.Type, c GeneratorConfig) bool {
	for _, pattern := range c.HideTypePatterns {
		if regexp.MustCompile(pattern).MatchString(t.Name.String()) {
			return true
		}
	}
	if !isExportedType(t) && unicode.IsLower(rune(t.Name.Name[0])) {
		// types that start with lowercase
		return true
	}
	return false
}

// finalUnderlyingTypeOf walks the type hierarchy for t and returns
// its base type (i.e. the type that has no further underlying type).
func finalUnderlyingTypeOf(t *types.Type) *types.Type {
	for {
		if t.Underlying == nil {
			return t
		}

		t = t.Underlying
	}
}

func typeReferences(t *types.Type, c GeneratorConfig, references map[*types.Type][]*types.Type) []*types.Type {
	var out []*types.Type
	m := make(map[*types.Type]struct{})
	for _, ref := range references[t] {
		if !hideType(ref, c) {
			m[ref] = struct{}{}
		}
	}
	for k := range m {
		out = append(out, k)
	}
	sortTypes(out)
	return out
}

func visibleTypes(in []*types.Type, c GeneratorConfig) []*types.Type {
	var out []*types.Type
	for _, t := range in {
		if !hideType(t, c) {
			out = append(out, t)
		}
	}
	return out
}
