package resolve

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-licenses/licenses"
	"golang.org/x/tools/go/packages"

	"github.com/tatskaari/go-deps/progress"
	"github.com/tatskaari/go-deps/resolve/knownimports"
	. "github.com/tatskaari/go-deps/resolve/model"
)

type Modules struct {
	Pkgs        map[string]*packages.Package
	Mods        map[string]*Module
	ImportPaths map[*packages.Package]*ModulePart
}

type resolver struct {
	*Modules
	moduleCounts   map[string]int
	rootModuleName string
	config         *packages.Config
	resolved       map[*packages.Package]struct{}
}

func newResolver(rootModuleName string, config *packages.Config) *resolver {
	return &resolver{
		Modules: &Modules{
			Pkgs:        map[string]*packages.Package{},
			Mods:        map[string]*Module{},
			ImportPaths: map[*packages.Package]*ModulePart{},
		},
		moduleCounts:   map[string]int{},
		rootModuleName: rootModuleName,
		config:         config,
	}
}

func (r *resolver) dependsOn(done map[*packages.Package]struct{}, pkg *packages.Package, module *ModulePart) bool {
	if _, ok := done[pkg]; ok {
		return false
	}
	done[pkg] = struct{}{}

	pkgModule := r.Import(pkg)
	if module == pkgModule {
		return true
	}
	for pkg := range pkgModule.Packages {
		for _, i := range pkg.Imports {
			if r.dependsOn(done, i, module) {
				return true
			}
		}
	}

	return false
}

// getOrCreateModulePart gets or create a module part that we can add this package to without causing a cycle
func (r *resolver) getOrCreateModulePart(m *Module, pkg *packages.Package) *ModulePart {
	var validPart *ModulePart
	for _, part := range m.Parts {
		valid := true
		done := map[*packages.Package]struct{}{}
		for _, i := range pkg.Imports {
			// Check all the imports that leave the current part
			if r.Import(i) != part {
				if r.dependsOn(done, i, part) {
					valid = false
					break
				}
			}
		}
		if valid {
			validPart = part
			break
		}
	}
	if validPart == nil {
		validPart = &ModulePart{
			Packages: map[*packages.Package]struct{}{},
			Module:   m,
			Index:    len(m.Parts) + 1,
		}
		m.Parts = append(m.Parts, validPart)
	}
	return validPart
}

func (r *resolver) addPackageToModuleGraph(done map[*packages.Package]struct{}, pkg *packages.Package) {
	if _, ok := done[pkg]; ok {
		return
	}

	for _, i := range pkg.Imports {
		r.addPackageToModuleGraph(done, i)
	}

	// We don't need to add the current module to the module graph
	if r.rootModuleName == pkg.Module.Path {
		return
	}

	part := r.getOrCreateModulePart(r.GetModule(pkg.Module), pkg)
	part.Packages[pkg] = struct{}{}
	r.ImportPaths[pkg] = part

	if !part.IsWildcardImport(pkg) {
		part.Modified = true
	}

	done[pkg] = struct{}{}
}

func getCurrentModuleName() string {
	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to get the current modules name: %v\n", err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (r *resolver) addPackagesToModules(done map[*packages.Package]struct{}) {
	processed := 0

	for _, pkg := range r.Pkgs {
		r.addPackageToModuleGraph(done, pkg)
		processed++
		progress.PrintUpdate("Building module graph... %d of %d packages.", processed, len(r.Pkgs))
	}
}

// UpdateModules resolves a `go get` style wildcard and updates the modules passed in to it
func UpdateModules(modules *Modules, getPaths []string, goListDriver packages.Driver) error {
	defer progress.Clear()

	pkgs, r, err := load(getPaths, goListDriver)
	if err != nil {
		return err
	}

	if r == nil {
		return nil
	}

	r.Modules = modules

	done := map[*packages.Package]struct{}{}
	if modules != nil {
		for _, pkg := range modules.Pkgs {
			done[pkg] = struct{}{}
		}
	}

	r.resolve(pkgs)
	r.addPackagesToModules(done)

	if err := r.resolveModifiedPackages(done); err != nil {
		return err
	}

	if err := r.setLicence(pkgs); err != nil {
		return err
	}

	return nil
}

func load(getPaths []string, driver packages.Driver) ([]*packages.Package, *resolver, error) {
	progress.PrintUpdate("Analysing packages...")

	config := &packages.Config{
		Mode:   packages.NeedImports | packages.NeedModule | packages.NeedName | packages.NeedFiles,
		Driver: driver,
	}
	r := newResolver(getCurrentModuleName(), config)

	pkgs, err := packages.Load(config, getPaths...)
	if err != nil {
		return nil, nil, err
	}

	errBuf := new(bytes.Buffer)
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			fmt.Fprintln(errBuf, err)
		}
	})

	if errString := errBuf.String(); errString != "" {
		return nil, nil, errors.New(errString)
	}

	return pkgs, r, nil
}

func (r *resolver) isResolved(pkg *packages.Package) bool {
	_, ok := r.resolved[pkg]
	return ok
}

func (r *resolver) resolveModifiedPackages(done map[*packages.Package]struct{}) error {
	var modifiedPackages []string
	for _, m := range r.Mods {
		if m.IsModified() {
			for _, part := range m.Parts {
				for pkg := range part.Packages {
					if !r.isResolved(pkg) {
						modifiedPackages = append(modifiedPackages, pkg.ID)
					}
				}
			}
		}
	}

	pkgs, err := packages.Load(r.config, modifiedPackages...)
	if err != nil {
		return err
	}

	r.resolve(pkgs)
	r.addPackagesToModules(done)
	return nil
}

func (r *resolver) resolve(pkgs []*packages.Package) {
	for _, p := range pkgs {
		if p.Module != nil {
			r.GetModule(p.Module).Version = p.Module.Version
		}
		if len(p.GoFiles)+len(p.OtherFiles) == 0 {
			continue
		}
		pkg := r.GetPackage(p.PkgPath)
		if p.Module == nil {
			if strings.HasPrefix(p.PkgPath, r.rootModuleName) {
				pkg.Module.Path = r.rootModuleName
			} else {
				var missingPkgs []string
				for _, pkg := range pkgs {
					if pkg.Module == nil {
						missingPkgs = append(missingPkgs, pkg.PkgPath)
					}
				}
				panic(fmt.Errorf("no module found for pkgs %v", missingPkgs))
			}
		} else {
			pkg.Module = p.Module
		}

		newPackages := make([]*packages.Package, 0, len(p.Imports))
		for importName, importedPkg := range p.Imports {
			if knownimports.IsInGoRoot(importName) {
				continue
			}
			newPkg := r.GetPackage(importName)
			if p.Module == nil {
				panic(fmt.Sprintf("no module for %v. Perhaps you need to run go mod download?", pkg.ID))
			}
			if importedPkg.Module == nil {
				panic(fmt.Sprintf("no module for imported package %v. Perhaps you need to run go mod download?", importedPkg.PkgPath))
			}
			if importedPkg.Module.Path != p.Module.Path {
				pkg.Imports[newPkg.ID] = newPkg
			}
			if !r.isResolved(newPkg) {
				newPackages = append(newPackages, importedPkg)
			}
		}
		r.resolved[pkg] = struct{}{}
		r.resolve(newPackages)
	}
}

func (mods *Modules) Import(pkg *packages.Package) *ModulePart {
	pkgModule, ok := mods.ImportPaths[pkg]
	if ok {
		return pkgModule
	}

	module, ok := mods.Mods[pkg.Module.Path]
	if !ok {
		panic(fmt.Errorf("no import path for pkg %v", pkg.ID))
	}
	for _, part := range module.Parts {
		if part.IsWildcardImport(pkg) {
			mods.ImportPaths[pkg] = part
			return part
		}
	}
	panic(fmt.Errorf("no import path for pkg %v", pkg.ID))
}

// GetPackage gets an existing package or creates a new one
func (mods *Modules) GetPackage(path string) *packages.Package {
	if pkg, ok := mods.Pkgs[path]; ok {
		return pkg
	}
	pkg := &packages.Package{ID: path, Imports: map[string]*packages.Package{}}
	mods.Pkgs[path] = pkg
	return pkg
}

func (mods *Modules) GetModule(mod *packages.Module) *Module {
	// TODO(jpoole): handle replace
	m, ok := mods.Mods[mod.Path]
	if !ok {
		m = &Module{
			Name: mod.Path,
		}
		mods.Mods[mod.Path] = m
	}
	return m
}

func (r *resolver) setLicence(pkgs []*packages.Package) (err error) {
	c, _ := licenses.NewClassifier(0.9)

	done := 0 // start at 1 to ignore the root module
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if err != nil {
			return
		}
		if _, ok := r.Pkgs[p.PkgPath]; !ok {
			return
		}
		var m *Module
		if p.Module == nil {
			if strings.HasPrefix(p.PkgPath, r.rootModuleName) {
				m = r.Mods[r.rootModuleName]
			} else {
				return
			}
		} else {
			m = r.Mods[p.Module.Path]
		}
		if !m.IsModified() {
			return
		}
		if m.Licence != "" || m.Name == r.rootModuleName {
			return
		}

		done++
		progress.PrintUpdate("Adding licenses... %d of %d modules.", done, len(r.Mods))

		var pkgDir string
		switch {
		case len(p.GoFiles) > 0:
			pkgDir = filepath.Dir(p.GoFiles[0])
		case len(p.CompiledGoFiles) > 0:
			pkgDir = filepath.Dir(p.CompiledGoFiles[0])
		case len(p.OtherFiles) > 0:
			pkgDir = filepath.Dir(p.OtherFiles[0])
		default:
			// This package is empty - nothing to do.
			return
		}

		path, e := licenses.Find(pkgDir, c)
		if e != nil {
			return
		}
		name, _, e := c.Identify(path)
		if e != nil {
			err = fmt.Errorf("failed to identify licence %v: %v", path, err)
			return
		}
		m.Licence = name
	})
	return
}
