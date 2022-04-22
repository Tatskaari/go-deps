package driver

import (
	"fmt"
	"github.com/tatskaari/go-deps/progress"
	"github.com/tatskaari/go-deps/resolve/driver/proxy"
	"go/build"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/tatskaari/go-deps/resolve/knownimports"
)

const dirPerms = os.ModeDir | 0775

var client = http.DefaultClient

type requirement struct {
	mod          *packages.Module
	replacements map[string]*modfile.Replace
}

type pleaseDriver struct {
	proxy              *proxy.Proxy
	thirdPartyFolder   string
	moduleRequirements map[string]*requirement
	pleaseModules      map[string]*goModDownloadRule

	goTool     string
	pleaseTool string

	packages map[string]*packages.Package

	downloaded map[string]string
}

type packageInfo struct {
	id              string
	srcRoot, pkgDir string
	mod             *requirement
	isSDKPackage    bool
}

func NewPleaseDriver(please, goTool, thirdPartyFolder string) *pleaseDriver {
	//TODO(jpoole): split this on , and get rid of direct
	proxyURL := os.Getenv("GOPROXY")
	if proxyURL == "" {
		proxyURL = "https://proxy.golang.org"
	}

	return &pleaseDriver{
		pleaseTool:       please,
		goTool:           goTool,
		thirdPartyFolder: thirdPartyFolder,
		proxy:            proxy.New(proxyURL),
		downloaded:       map[string]string{},
		pleaseModules:    map[string]*goModDownloadRule{},
	}
}

func (driver *pleaseDriver) pkgInfo(from *requirement, id string) (*packageInfo, error) {
	if knownimports.IsInGoRoot(id) {
		srcDir := filepath.Join(build.Default.GOROOT, "src")
		return &packageInfo{isSDKPackage: true, id: id, srcRoot: srcDir, pkgDir: filepath.Join(srcDir, id)}, nil
	}

	if info, err := driver.checkReplace(from, id); info != nil || err != nil {
		return info, err
	}

	mod, err := driver.ModuleForPackage(id)
	if err != nil {
		return nil, fmt.Errorf("no module requirement for %v", err)
	}

	srcRoot, err := driver.ensureDownloaded(mod.mod)
	if err != nil {
		return nil, err
	}

	pkgDir := strings.TrimPrefix(id, mod.mod.Path)
	return &packageInfo{
		id:      id,
		srcRoot: srcRoot,
		pkgDir:  filepath.Join(srcRoot, pkgDir),
		mod:     mod,
	}, nil
}

func (driver *pleaseDriver) checkReplace(from *requirement, id string) (*packageInfo, error) {
	if from == nil {
		return nil, nil
	}

	check := func(req *modfile.Replace) (*packageInfo, error) {
		if strings.HasPrefix(id, req.Old.Path) {
			ver := req.New

			mod := driver.moduleRequirements[ver.Path]

			srcRoot, err := driver.ensureDownloaded(mod.mod)
			if err != nil {
				return nil, err
			}
			pkgDir := filepath.Join(srcRoot, strings.TrimPrefix(id, req.Old.Path))
			if _, err := os.Stat(pkgDir); err == nil {
				old := &requirement{mod: &packages.Module{Path: req.Old.Path, Version: req.Old.Version, Replace: mod.mod}}

				return &packageInfo{
					id:      id,
					srcRoot: srcRoot,
					pkgDir:  pkgDir,
					mod:     old,
				}, nil
			}
		}
		return nil, nil
	}

	if from.mod.Replace != nil {
		replace := &modfile.Replace{
			Old: module.Version{Path: from.mod.Path, Version: from.mod.Version},
			New: module.Version{Path: from.mod.Replace.Path, Version: from.mod.Replace.Version},
		}

		if info, err := check(replace); info != nil || err != nil {
			return info, err
		}
	}

	for _, req := range from.replacements {
		if info, err := check(req); info != nil || err != nil {
			return info, err
		}
	}
	return nil, nil
}

// loadPattern will load a package wildcard into driver.packages, walking the directory tree if necessary
func (driver *pleaseDriver) loadPattern(pattern string) ([]string, error) {
	walk := strings.HasSuffix(pattern, "...")

	info, err := driver.pkgInfo(nil, strings.TrimSuffix(pattern, "/..."))
	if err != nil {
		return nil, err
	}

	if walk {
		var roots []string

		err := filepath.Walk(info.pkgDir, func(path string, i fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !i.IsDir() {
				return nil
			}

			if strings.HasPrefix(i.Name(), ".") {
				return fs.SkipDir
			}

			id := filepath.Join(info.mod.mod.Path, strings.TrimPrefix(path, info.srcRoot))
			info, err := driver.pkgInfo(nil, strings.TrimSuffix(id, "/..."))
			if err != nil {
				return err
			}

			if err := driver.loadPackage(info); err != nil {
				if _, ok := err.(*build.NoGoError); ok || strings.HasPrefix(err.Error(), "no buildable Go source files in ") {
					return nil
				}
				return err
			}
			roots = append(roots, id)
			return nil
		})
		return roots, err
	} else {
		return []string{info.id}, driver.loadPackage(info)
	}
}

// loadPackage will parse a go package's sources to find out what it imports and load them into driver.packages
func (driver *pleaseDriver) loadPackage(info *packageInfo) error {
	if _, ok := driver.packages[info.id]; ok {
		return nil
	}

	if knownimports.IsInGoRoot(info.id) {
		return nil
	}

	progress.PrintUpdate("Analysing %v", info.id)
	pkg, err := build.ImportDir(info.pkgDir, build.ImportComment)
	if err != nil {
		return fmt.Errorf("%v from %v", err, info.id)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("%v from %v", err, info.id)
	}

	imports := map[string]*packages.Package{}
	for _, i := range pkg.Imports {
		if i == "C" {
			return nil
		}
		imports[i] = &packages.Package{ID: i}
		newInfo, err := driver.pkgInfo(info.mod, i)
		if err != nil {
			return fmt.Errorf("%v from %v from %v", err, i, info.id)
		}
		if err := driver.loadPackage(newInfo); err != nil {
			return fmt.Errorf("%v from %v", err, info.id)
		}
	}

	goFiles := make([]string, 0, len(pkg.GoFiles))
	for _, f := range pkg.GoFiles {
		goFiles = append(goFiles, filepath.Join(wd, info.pkgDir, f))
	}

	driver.packages[info.id] = &packages.Package{
		ID:      info.id,
		Name:    pkg.Name,
		PkgPath: info.id,
		GoFiles: goFiles,
		Imports: imports,
		Module:  info.mod.mod,
	}
	return nil
}

func (driver *pleaseDriver) Resolve(cfg *packages.Config, patterns ...string) (*packages.DriverResponse, error) {
	driver.packages = map[string]*packages.Package{}
	driver.moduleRequirements = map[string]*requirement{}

	if err := os.MkdirAll("plz-out/godeps", dirPerms); err != nil && !os.IsExist(err) {
		return nil, err
	}

	pkgWildCards, err := driver.resolveGetModules(patterns)
	if err != nil {
		return nil, err
	}

	if err := driver.loadPleaseModules(); err != nil {
		return nil, err
	}

	resp := new(packages.DriverResponse)
	for _, p := range pkgWildCards {
		pkgs, err := driver.loadPattern(p)
		if err != nil {
			return nil, err
		}
		resp.Roots = append(resp.Roots, pkgs...)
	}

	resp.Packages = make([]*packages.Package, 0, len(driver.packages))
	for _, pkg := range driver.packages {
		resp.Packages = append(resp.Packages, pkg)
	}
	return resp, nil
}
