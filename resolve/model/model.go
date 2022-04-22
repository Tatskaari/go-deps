package model

import (
	"golang.org/x/tools/go/packages"
	"path/filepath"
	"strings"
)

// Module represents a module. It includes all deps so actually represents a full module graph.
type Module struct {
	// The module name
	Name       string
	ReplacedBy string
	Version    string
	Licence    string

	// TODO(jpoole): Store these on the resolver and use packages.Module instead of this struct
	Parts []*ModulePart
}

func (m *Module) IsModified() bool {
	for _, part := range m.Parts {
		if part.Modified {
			return true
		}
	}
	return false
}

// ModulePart essentially corresponds to a `go_module()` rule that compiles some (or all) packages from that module. In
// most cases, there's one part per module except where we need to split it out to resolve a cycle.
type ModulePart struct {
	Module *Module

	// Any packages in the install list matched with "..." N.B the package doesn't have the /... on the end
	InstallWildCards []string

	// The packages in this module
	Packages map[*packages.Package]struct{}
	// The index of this module part
	Index int

	Modified bool
}

func (p *ModulePart) IsWildcardImport(pkg *packages.Package) bool {
	return p.GetWildcardImport(pkg) != ""
}

func (p *ModulePart) GetWildcardImport(pkg *packages.Package) string {
	for _, i := range p.InstallWildCards {
		wildCardPath := filepath.Join(pkg.Module.Path, i)
		if strings.HasPrefix(pkg.ID, wildCardPath) {
			return filepath.Join(i, "...")
		}
	}
	return ""
}
