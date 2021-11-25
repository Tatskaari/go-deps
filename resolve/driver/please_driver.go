package driver

import (
	"fmt"
	"go/build"
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

type pleaseDriver struct {
	moduleProxy        string
	thirdPartyFolder   string
	pleasePath         string
	moduleRequirements map[string]*packages.Module
	knownModules 	   []string
	pleaseModules      map[string]*goModDownloadRule

	packages map[string]*packages.Package

	downloaded map[string]string
}

type packageInfo struct {
	id              string
	srcRoot, pkgDir string
	mod             *packages.Module
	isSDKPackage    bool
}

func NewPleaseDriver(please, thirdPartyFolder string) *pleaseDriver {
	//TODO(jpoole): split this on , and get rid of direct
	proxy := os.Getenv("GOPROXY")
	if proxy == "" {
		proxy = "https://proxy.golang.org"
	}

	return &pleaseDriver{
		pleasePath: please,
		thirdPartyFolder: thirdPartyFolder,
		moduleProxy:      proxy,
		downloaded:       map[string]string{},
		pleaseModules:    map[string]*goModDownloadRule{},
	}
}

func (driver *pleaseDriver) pkgInfo(id string) (*packageInfo, error) {
	if knownimports.IsInGoRoot(id) {
		srcDir := filepath.Join(build.Default.GOROOT, "src")
		return &packageInfo{isSDKPackage: true, id: id, srcRoot: srcDir, pkgDir: filepath.Join(srcDir, id)}, nil
	}

	modName, err := driver.resolveModuleForPackage(id)
	if err != nil {
		return nil, err
	}

	mod, ok := driver.moduleRequirements[modName]
	if !ok {
		return nil, fmt.Errorf("no module requirement for %v", modName)
	}

	srcRoot, err := driver.ensureDownloaded(mod)
	if err != nil {
		return nil, err
	}

	return &packageInfo{
		id:      id,
		srcRoot: srcRoot,
		pkgDir:  filepath.Join(srcRoot, strings.TrimPrefix(id, mod.Path)),
		mod:     mod,
	}, nil
}

// loadPattern will load a package wildcard into driver.packages, walking the directory tree if necessary
func (driver *pleaseDriver) loadPattern(pattern string) ([]string, error) {
	walk := strings.HasSuffix(pattern, "...")

	info, err := driver.pkgInfo(strings.TrimSuffix(pattern, "/..."))
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

			id := filepath.Join(info.mod.Path, strings.TrimPrefix(path, info.srcRoot))
			info, err := driver.pkgInfo(strings.TrimSuffix(id, "/..."))
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
		newInfo, err := driver.pkgInfo(i)
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
		Module:  info.mod,
	}
	return nil
}

func (driver *pleaseDriver) Resolve(cfg *packages.Config, patterns ...string) (*packages.DriverResponse, error) {
	driver.packages = map[string]*packages.Package{}
	driver.moduleRequirements = map[string]*packages.Module{}

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
