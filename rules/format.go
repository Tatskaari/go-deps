package rules

import (
	"fmt"
	"golang.org/x/tools/go/packages"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	resolve "github.com/tatskaari/go-deps/resolve/model"

	"github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/buildtools/edit"
	"github.com/bazelbuild/buildtools/tables"
)

var semverRegex = regexp.MustCompile("^v[0-9]+$")

func split(path string) (string, string) {
	dir, base := filepath.Split(path)
	return filepath.Clean(dir), base
}

func (file *BuildFile) assignName(mod *resolve.Module, suffix string, structured bool) string {
	path, base := split(mod.Name)
	name := base + suffix
	if mod.ReplacedBy != "" {
		name = base + "_replace" + suffix
	}
	if !structured && semverRegex.MatchString(name) {
		path, base = split(path)
		name = base + "." + name
	}

	for {
		extantPath, ok := file.usedNames[name]
		if !ok {
			break
		}
		if extantPath == mod.Name {
			return name
		}
		path, base = split(path)
		name = base + "." + name
	}
	file.usedNames[name] = mod.Name
	return name
}

func (file *BuildFile) partName(part *resolve.ModulePart, structured bool) string {
	if name, ok := file.partNames[part]; ok {
		return name
	}
	displayIndex := len(part.Module.Parts) - part.Index
	suffix := ""
	if displayIndex > 0 {
		suffix = fmt.Sprintf("_%d", displayIndex)
	}

	name := file.assignName(part.Module, suffix, structured)
	file.partNames[part] = name
	return name
}

func (file *BuildFile) downloadRuleName(module *resolve.Module, structured bool) string {
	if name, ok := file.downloadNames[module]; ok {
		return name
	}
	name := file.assignName(module, "_dl", structured)
	file.downloadNames[module] = name
	return name
}

func toInstall(pkg *packages.Package) string {
	install := strings.Trim(strings.TrimPrefix(pkg.ID, pkg.Module.Path), "/")
	if install == "" {
		return "."
	}
	return install
}

func (g *BuildGraph) file(mod *resolve.Module, structured bool, thirdPartyFolder string) (*BuildFile, error) {
	path := ""
	if structured {
		path = filepath.Join(thirdPartyFolder, mod.Name, g.BuildFileName)
	} else {
		path = filepath.Join(thirdPartyFolder, g.BuildFileName)
	}

	if f, ok := g.Files[path]; ok {
		g.ModFiles[mod] = f
		return f, nil
	} else {
		// TODO create the build file
		file, err := newFile(path)
		if err != nil {
			return nil, err
		}

		g.ModFiles[mod] = file
		g.Files[path] = file

		return file, nil
	}
}

func cannonicalise(name, modpath, thirdParty string, structured bool) string {
	if !structured {
		return ":" + name
	}

	return "//" + filepath.Join(thirdParty, modpath) + ":" + name
}

func (g *BuildGraph) Format(structured, write bool, thirdPartyFolder string) error {
	for _, m := range g.Modules.Mods {
		file, err := g.file(m, structured, thirdPartyFolder)
		if err != nil {
			return err
		}
		dlRule, ok := file.ModDownloadRules[m]
		if len(m.Parts) > 1 || m.ReplacedBy != "" {
			if !ok {
				dlRule = NewRule(file.File, "go_mod_download", file.downloadRuleName(m, structured))
				file.ModDownloadRules[m] = dlRule
			}
			name := m.Name
			version := m.Version
			if m.ReplacedBy != "" {
				name = m.ReplacedBy
			}

			dlRule.SetAttr("module", NewStringExpr(name))
			if m.Version != "" {
				dlRule.SetAttr("version", NewStringExpr(version))
			}
			if m.Licence != "" {
				dlRule.SetAttr("licences", NewStringList(m.Licence))
			}
		}

		for _, part := range m.Parts {
			if !part.Modified {
				continue
			}
			modRule, ok := file.ModRules[part]
			if !ok {
				modRule = NewRule(file.File, "go_module", file.partName(part, structured))
				file.ModRules[part] = modRule
			}
			modRule.DelAttr("install")
			modRule.DelAttr("deps")
			modRule.DelAttr("exported_deps")
			modRule.DelAttr("visibility")

			modRule.SetAttr("module", NewStringExpr(m.Name))

			if dlRule != nil {
				modRule.DelAttr("version")
				modRule.SetAttr("download", NewStringExpr(":"+file.downloadRuleName(m, structured)))
			} else {
				if m.Licence != "" {
					modRule.SetAttr("licences", NewStringList(m.Licence))
				}
				if m.Version != "" {
					modRule.SetAttr("version", NewStringExpr(m.Version))
				}
			}

			installs := make([]string, 0, len(part.Packages))
			deps := make([]string, 0, len(part.Packages))
			var exportedDeps []string

			doneDeps := map[string]struct{}{}
			doneInstalls := map[string]struct{}{}

			for _, i := range part.InstallWildCards {
				installs = append(installs, i+"/...")
			}

			for pkg := range part.Packages {
				if part.IsWildcardImport(pkg) {
					continue
				}
				i := toInstall(pkg)
				if _, ok := doneInstalls[i]; !ok {
					installs = append(installs, i)
					doneInstalls[i] = struct{}{}
				}

				for _, i := range pkg.Imports {
					dep := g.Modules.Import(i)
					depFile, err := g.file(dep.Module, structured, thirdPartyFolder)
					if err != nil {
						return err
					}
					depRuleName := depFile.partName(dep, structured)
					depRule := cannonicalise(depRuleName, dep.Module.Name, thirdPartyFolder, structured)
					if _, ok := doneDeps[depRule]; ok || dep.Module == m {
						continue
					}
					doneDeps[depRule] = struct{}{}
					deps = append(deps, depRule)
				}
			}

			// The last part is the namesake and should export the rest of the parts.
			if part.Index == len(m.Parts) {
				modRule.SetAttr("visibility", NewStringList("PUBLIC"))

				for _, part := range m.Parts[:(len(m.Parts) - 1)] {
					exportedDeps = append(exportedDeps, ":"+file.partName(part, structured))
				}
			} else {
				if structured {
					modRule.SetAttr("visibility", NewStringList("PUBLIC"))
				}
			}

			if len(installs) > 1 || (len(installs) == 1 && installs[0] != ".") {
				modRule.SetAttr("install", NewStringList(installs...))
			}

			if len(deps) > 0 {
				modRule.SetAttr("deps", NewStringList(deps...))
			}

			if len(exportedDeps) > 0 {
				modRule.SetAttr("exported_deps", NewStringList(exportedDeps...))
			}

		}
	}

	tables.IsSortableListArg["install"] = true
	for path, f := range g.Files {
		if write {
			if err := os.MkdirAll(filepath.Dir(f.File.Path), os.ModeDir|0775); err != nil {
				return err
			}

			osFile, err := os.Create(f.File.Path)
			if err != nil {
				return err
			}

			if _, err := osFile.Write(build.Format(f.File)); err != nil {
				return err
			}
			osFile.Close()
		} else {
			fmt.Println("# " + path)
			fmt.Println(string(build.Format(f.File)))
		}

	}
	return nil
}

func NewRule(f *build.File, kind, name string) *build.Rule {
	rule, _ := edit.ExprToRule(&build.CallExpr{
		X:    &build.Ident{Name: kind},
		List: []build.Expr{},
	}, kind)

	rule.SetAttr("name", NewStringExpr(name))

	f.Stmt = append(f.Stmt, rule.Call)
	return rule
}

func NewStringExpr(s string) *build.StringExpr {
	return &build.StringExpr{Value: s}
}

func NewStringList(ss ...string) *build.ListExpr {
	l := new(build.ListExpr)
	for _, s := range ss {
		l.List = append(l.List, NewStringExpr(s))
	}
	return l
}
