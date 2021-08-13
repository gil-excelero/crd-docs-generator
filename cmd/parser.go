package main

import (
	"fmt"
	"html/template"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/russross/blackfriday/v2"
	"k8s.io/gengo/parser"
	"k8s.io/gengo/types"
	"k8s.io/klog"
)

var (
	safeIDRegex = regexp.MustCompile("[[:punct:]]+")
)

const (
	docCommentForceIncludes = "// +gencrdrefdocs:force"
)

type GeneratorConfig struct {
	// HiddenMemberFields hides fields with specified names on all types.
	HiddenMemberFields []string `json:"hideMemberFields"`

	// HideTypePatterns hides types matching the specified patterns from the
	// output.
	HideTypePatterns []string `json:"hideTypePatterns"`

	// ExternalPackages lists recognized external package references and how to
	// link to them.
	ExternalPackages []externalPackage `json:"externalPackages"`

	// TypeDisplayNamePrefixOverrides is a mapping of how to override displayed
	// name for types with certain prefixes with what value.
	TypeDisplayNamePrefixOverrides map[string]string `json:"typeDisplayNamePrefixOverrides"`

	// MarkdownDisabled controls markdown rendering for comment lines.
	MarkdownDisabled bool `json:"markdownDisabled"`

	// PreserveTrailingWhitespace controls retention of trailing whitespace in the rendered document.
	PreserveTrailingWhitespace bool `json:"preserveTrailingWhitespace"`

	// GitCommitDisabled causes the git commit information to be excluded from the output.
	GitCommitDisabled bool `json:"gitCommitDisabled"`
}

type externalPackage struct {
	TypeMatchPrefix string `json:"typeMatchPrefix"`
	DocsURLTemplate string `json:"docsURLTemplate"`
}

type apiPackage struct {
	apiGroup   string
	apiVersion string
	GoPackages []*types.Package
	Types      []*types.Type // because multiple 'types.Package's can add types to an apiVersion
	Constants  []*types.Type
}

func (v *apiPackage) identifier() string { return fmt.Sprintf("%s/%s", v.apiGroup, v.apiVersion) }

// groupName extracts the "//+groupName" meta-comment from the specified
// package's comments, or returns empty string if it cannot be found.
func groupName(pkg *types.Package) string {
	m := types.ExtractCommentTags("+", pkg.Comments)
	v := m["groupName"]
	if len(v) == 1 {
		return v[0]
	}
	return ""
}

func ParseAPIPackages(dir string) ([]*types.Package, error) {
	b := parser.New()
	// the following will silently fail (turn on -v=4 to see logs)
	if err := b.AddDirRecursive(*flAPIDir); err != nil {
		return nil, err
	}
	scan, err := b.FindTypes()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse pkgs and types")
	}
	var pkgNames []string
	for p := range scan {
		pkg := scan[p]
		klog.V(3).Infof("trying package=%v groupName=%s", p, groupName(pkg))

		// Do not pick up packages that are in vendor/ as API packages. (This
		// happened in knative/eventing-sources/vendor/..., where a package
		// matched the pattern, but it didn't have a compatible import path).
		if isVendorPackage(pkg) {
			klog.V(3).Infof("package=%v coming from vendor/, ignoring.", p)
			continue
		}

		if groupName(pkg) != "" && len(pkg.Types) > 0 || containsString(pkg.DocComments, docCommentForceIncludes) {
			klog.V(3).Infof("package=%v has groupName and has types", p)
			pkgNames = append(pkgNames, p)
		}
	}
	sort.Strings(pkgNames)
	var pkgs []*types.Package
	for _, p := range pkgNames {
		klog.Infof("using package=%s", p)
		pkgs = append(pkgs, scan[p])
	}
	return pkgs, nil
}

func containsString(sl []string, str string) bool {
	for _, s := range sl {
		if str == s {
			return true
		}
	}
	return false
}

// combineAPIPackages groups the Go packages by the <apiGroup+apiVersion> they
// offer, and combines the types in them.
func combineAPIPackages(pkgs []*types.Package) ([]*apiPackage, error) {
	pkgMap := make(map[string]*apiPackage)
	var pkgIds []string

	flattenTypes := func(typeMap map[string]*types.Type) []*types.Type {
		typeList := make([]*types.Type, 0, len(typeMap))

		for _, t := range typeMap {
			typeList = append(typeList, t)
		}

		return typeList
	}

	for _, pkg := range pkgs {
		apiGroup, apiVersion, err := apiVersionForPackage(pkg)
		if err != nil {
			return nil, errors.Wrapf(err, "could not get apiVersion for package %s", pkg.Path)
		}

		typeList := make([]*types.Type, 0, len(pkg.Types))
		for _, t := range pkg.Types {
			typeList = append(typeList, t)
		}

		id := fmt.Sprintf("%s/%s", apiGroup, apiVersion)
		v, ok := pkgMap[id]
		if !ok {
			pkgMap[id] = &apiPackage{
				apiGroup:   apiGroup,
				apiVersion: apiVersion,
				Types:      flattenTypes(pkg.Types),
				Constants:  flattenTypes(pkg.Constants),
				GoPackages: []*types.Package{pkg},
			}
			pkgIds = append(pkgIds, id)
		} else {
			v.Types = append(v.Types, flattenTypes(pkg.Types)...)
			v.Constants = append(v.Types, flattenTypes(pkg.Constants)...)
			v.GoPackages = append(v.GoPackages, pkg)
		}
	}

	sort.Sort(sort.StringSlice(pkgIds))

	out := make([]*apiPackage, 0, len(pkgMap))
	for _, id := range pkgIds {
		out = append(out, pkgMap[id])
	}
	sortPackages(out)

	return out, nil
}

// sortPackages sorts the given packages in a consistent alphabetical order.
func sortPackages(packages []*apiPackage) {
	sort.SliceStable(packages, func(i, j int) bool {
		a := packages[i]
		b := packages[j]
		if a.apiGroup != b.apiGroup {
			return a.apiGroup < b.apiGroup
		}
		return a.apiVersion < b.apiVersion
	})
}

// isVendorPackage determines if package is coming from vendor/ dir.
func isVendorPackage(pkg *types.Package) bool {
	vendorPattern := string(os.PathSeparator) + "vendor" + string(os.PathSeparator)
	return strings.Contains(pkg.SourcePath, vendorPattern)
}

func isExportedType(t *types.Type) bool {
	// TODO(ahmetb) use types.ExtractSingleBoolCommentTag() to parse +genclient
	// https://godoc.org/k8s.io/gengo/types#ExtractCommentTags
	exportedRegEx := regexp.MustCompile(`\+(genclient|kubebuilder:object:root=true)`)
	for _, line := range t.SecondClosestCommentLines {
		if exportedRegEx.MatchString(line) {
			return true
		}
	}
	return false
}

func fieldName(m types.Member) string {
	v := reflect.StructTag(m.Tags).Get("json")
	v = strings.TrimSuffix(v, ",omitempty")
	v = strings.TrimSuffix(v, ",inline")
	if v != "" && v != "-" {
		return v
	}
	return m.Name
}

func fieldEmbedded(m types.Member) bool {
	return strings.Contains(reflect.StructTag(m.Tags).Get("json"), ",inline")
}

func isLocalType(t *types.Type, typePkgMap map[*types.Type]*apiPackage) bool {
	t = tryDereference(t)
	_, ok := typePkgMap[t]
	return ok
}

func renderComments(s []string, markdown bool) string {
	s = filterCommentTags(s)
	doc := strings.Join(s, "\n")

	if markdown {
		// TODO(ahmetb): when a comment includes stuff like "http://<service>"
		// we treat this as a HTML tag with markdown renderer below. solve this.
		return string(blackfriday.Run([]byte(doc)))
	}
	return doc
}

func safe(s string) template.HTML { return template.HTML(s) }

func hiddenMember(m types.Member, c GeneratorConfig) bool {
	for _, v := range c.HiddenMemberFields {
		if m.Name == v {
			return true
		}
	}
	return false
}

// apiGroupForType looks up apiGroup for the given type
func apiGroupForType(t *types.Type, typePkgMap map[*types.Type]*apiPackage) string {
	t = tryDereference(t)

	v := typePkgMap[t]
	if v == nil {
		klog.Warningf("WARNING: cannot read apiVersion for %s from type=>pkg map", t.Name.String())
		return "<UNKNOWN_API_GROUP>"
	}

	return v.identifier()
}

func typeIdentifier(t *types.Type) string {
	t = tryDereference(t)
	return t.Name.String() // {PackagePath.Name}
}

// tryDereference returns the underlying type when t is a pointer, map, or slice.
func tryDereference(t *types.Type) *types.Type {
	for t.Elem != nil {
		t = t.Elem
	}
	return t
}

func sortTypes(typs []*types.Type) []*types.Type {
	sort.Slice(typs, func(i, j int) bool {
		t1, t2 := typs[i], typs[j]
		if isExportedType(t1) && !isExportedType(t2) {
			return true
		} else if !isExportedType(t1) && isExportedType(t2) {
			return false
		}
		return t1.Name.String() < t2.Name.String()
	})
	return typs
}

func filterCommentTags(comments []string) []string {
	var out []string
	for _, v := range comments {
		if !strings.HasPrefix(strings.TrimSpace(v), "+") {
			out = append(out, v)
		}
	}
	return out
}

func isOptionalMember(m types.Member) bool {
	tags := types.ExtractCommentTags("+", m.CommentLines)
	_, ok := tags["optional"]
	return ok
}

func apiVersionForPackage(pkg *types.Package) (string, string, error) {
	group := groupName(pkg)
	version := pkg.Name // assumes basename (i.e. "v1" in "core/v1") is apiVersion
	r := `^v\d+((alpha|beta)\d+)?$`
	if !regexp.MustCompile(r).MatchString(version) {
		return "", "", errors.Errorf("cannot infer kubernetes apiVersion of go package %s (basename %q doesn't match expected pattern %s that's used to determine apiVersion)", pkg.Path, version, r)
	}
	return group, version, nil
}

// packageMapToList flattens the map.
func packageMapToList(pkgs map[string]*apiPackage) []*apiPackage {
	out := make([]*apiPackage, 0, len(pkgs))
	for _, v := range pkgs {
		out = append(out, v)
	}
	return out
}

// constantsOfType finds all the constants in pkg that have the
// same underlying type as t. This is intended for use by enum
// type validation, where users need to specify one of a specific
// set of constant values for a field.
func constantsOfType(t *types.Type, pkg *apiPackage) []*types.Type {
	constants := []*types.Type{}

	for _, c := range pkg.Constants {
		if c.Underlying == t {
			constants = append(constants, c)
		}
	}

	return sortTypes(constants)
}
