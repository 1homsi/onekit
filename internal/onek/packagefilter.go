package onek

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/1homsi/onekit/internal/onkir"
)

func packageLabel(relDir string) string {
	slashed := filepath.ToSlash(relDir)
	if slashed == "" {
		return "."
	}
	return slashed
}

func (f ServiceFilter) packageFilterActive() bool {
	return len(f.IncludePackages) > 0 || len(f.ExcludePackages) > 0
}

func (f ServiceFilter) keepsPackage(relDir string) bool {
	label := packageLabel(relDir)
	if len(f.IncludePackages) > 0 && !matchesAny(f.IncludePackages, label) {
		return false
	}
	return !matchesAny(f.ExcludePackages, label)
}

func (idx *sourceIndex) keeping(keep func(relDir string) bool) *sourceIndex {
	view := *idx
	view.groups = nil
	for _, group := range idx.groups {
		if keep(group.relDir) {
			view.groups = append(view.groups, group)
		}
	}
	return &view
}

func (idx *sourceIndex) view(f ServiceFilter) *sourceIndex {
	if !f.packageFilterActive() {
		return idx
	}
	return idx.keeping(f.keepsPackage)
}

func (idx *sourceIndex) checkSelfContained(target string, view *sourceIndex) error {
	if len(view.groups) == len(idx.groups) {
		return nil
	}
	kept := map[string]bool{}
	for _, group := range view.groups {
		kept[packageLabel(group.relDir)] = true
	}
	for _, group := range view.groups {
		if err := idx.checkGroupRefs(target, group, kept); err != nil {
			return err
		}
	}
	return nil
}

func (idx *sourceIndex) checkGroupRefs(target string, group *sourceGroup, kept map[string]bool) error {
	from := packageLabel(group.relDir)
	var failure error
	fail := func(dir, name string) {
		if failure == nil && dir != "" && !kept[packageLabel(dir)] {
			failure = fmt.Errorf("%s: package %s uses %s from package %s, which include_packages/exclude_packages leaves out", target, from, name, packageLabel(dir))
		}
	}
	seen := map[*onkir.Message]bool{}
	var visitType func(*onkir.Type)
	var visitMessage func(*onkir.Message)
	visitType = func(typ *onkir.Type) {
		if typ == nil {
			return
		}
		switch typ.Kind {
		case onkir.KindMessage:
			visitMessage(typ.Message)
		case onkir.KindEnum:
			if typ.Enum != nil {
				if dir, ok := idx.dirByEnum[typ.Enum]; ok {
					fail(dir, typ.Enum.Name)
				}
			}
		case onkir.KindMap:
			visitType(typ.MapValue)
		}
	}
	visitMessage = func(m *onkir.Message) {
		if m == nil || seen[m] {
			return
		}
		seen[m] = true
		if dir, ok := idx.dirByMessage[m]; ok {
			fail(dir, m.Name)
		}
		for _, field := range m.Fields {
			visitType(field.Type)
			if field.Oneof != nil {
				for _, variant := range field.Oneof.Variants {
					visitType(variant.Type)
				}
			}
		}
	}
	for _, m := range group.file.Messages {
		visitMessage(m)
	}
	for _, service := range group.file.Services {
		for _, method := range service.Methods {
			visitMessage(method.Request)
			visitMessage(method.Response)
			for _, errType := range method.ErrorTypes {
				visitMessage(errType)
			}
		}
	}
	return failure
}

func validatePackagePatterns(target string, f ServiceFilter) error {
	for _, field := range []struct {
		name     string
		patterns []string
	}{{"include_packages", f.IncludePackages}, {"exclude_packages", f.ExcludePackages}} {
		for _, pattern := range field.patterns {
			if _, err := path.Match(pattern, ""); err != nil {
				return fmt.Errorf("%s %s: invalid pattern %q: %w", target, field.name, pattern, err)
			}
		}
	}
	return nil
}

func (f ServiceFilter) checkPackageMatches(target string, idx *sourceIndex) error {
	for _, field := range []struct {
		name     string
		patterns []string
	}{{"include_packages", f.IncludePackages}, {"exclude_packages", f.ExcludePackages}} {
		for _, pattern := range field.patterns {
			found := false
			for _, group := range idx.groups {
				if matched, _ := path.Match(pattern, packageLabel(group.relDir)); matched {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("%s %s pattern %q matches no package", target, field.name, pattern)
			}
		}
	}
	return nil
}
