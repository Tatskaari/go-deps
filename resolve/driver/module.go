package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/packages"

	"github.com/tatskaari/go-deps/progress"
)

// goModDownloadRule represents a `go_mod_download()` rule from Please BUILD files
type goModDownloadRule struct {
	label   string
	built   bool
	srcRoot string
}

// ensureDownloaded ensures the module has been downloaded and returns the filepath to its source root
func (driver *pleaseDriver) ensureDownloaded(mod *packages.Module) (srcRoot string, err error) {
	// TODO(jpoole): walk the module srcs tree to find all known packages for this module to avoid hitting the proxy
	key := fmt.Sprintf("%v@%v", mod.Path, mod.Version)
	if path, ok := driver.downloaded[key]; ok {
		return path, nil
	}

	// Try downloading using Please first
	if target, ok := driver.pleaseModules[mod.Path]; ok {
		if target.built {
			return target.srcRoot, nil
		}
		cmd := exec.Command(driver.pleaseTool, "build", target.label)
		progress.PrintUpdate("Building %s...", target.label)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to build %v: %v\n%v", target.label, err, string(out))
		}

		target.built = true
		return target.srcRoot, nil
	}

	// Create a dummy go.mod to avoid us accidentally updating the main repo
	if _, err := os.Lstat("plz-out/godeps/go.mod"); err != nil {
		if os.IsNotExist(err) {
			cmd := exec.Command(driver.goTool, "mod", "init", "dummy")
			cmd.Dir = "plz-out/godeps"
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("failed to create dummy mod: %v\n%v", err, string(out))
			}
		} else {
			return "", err
		}
	}

	var resp = struct {
		Path    string
		GoMod   string
		Version string
		Dir     string
		Error   string
	}{}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Downlaod using `go mod download`
	cmd := exec.Command(driver.goTool, "mod", "download", "--json", key)
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOROOT=%s", goroot))
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("GOPATH=%s", filepath.Join(wd, "plz-out/godeps/go")))
	cmd.Dir = "plz-out/godeps"
	progress.PrintUpdate("Downloading %s...", key)
	out, err := cmd.CombinedOutput()

	if err != nil {
		// Ignore this. Parsing the body is best effort to get the error message out.
		_ = json.Unmarshal(out, &resp)
		errorString := string(out)
		if resp.Error != "" {
			s, e := strconv.Unquote(resp.Error)
			if e == nil {
				errorString = s
			}
		}
		return "", fmt.Errorf("failed to download module %v: %v\n%v", key, err, errorString)
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", err
	}

	driver.downloaded[key] = resp.Dir

	return resp.Dir, nil
}

// determineVersionRequirements loads the version requirements from the go.mod files for each module, and applies
// the minimum valid version algorithm.
func (driver *pleaseDriver) determineVersionRequirements(mod, ver string) error {
	if oldReq, ok := driver.moduleRequirements[mod]; ok {
		// if we already require at this version or higher, we don't need to do anything
		if semver.Compare(ver, oldReq.mod.Version) <= 0 {
			return nil
		}
	}

	if mod == "" {
		panic(mod)
	}

	progress.PrintUpdate("Resolving %v@%v", mod, ver)

	modFile, err := driver.proxy.GetGoMod(mod, ver)
	if err != nil {
		ver := fmt.Sprintf("%v-incompatible", ver)
		modFile, err = driver.proxy.GetGoMod(mod, ver)
		if err != nil {
			return err
		}
	}

	driver.moduleRequirements[mod] = &requirement{
		mod: &packages.Module{Path: mod, Version: ver},
	}

	replacements := make(map[string]*modfile.Replace)
	for _, r := range modFile.Replace {
		newPath := r.New.Path
		newVer := r.New.Version

		if newVer == "" {
			newVer = ver
		}

		if strings.HasPrefix(newPath, ".") {
			newPath = filepath.Join(mod, newPath)
		}

		if err := driver.determineVersionRequirements(newPath, newVer); err == nil {
			replacements[r.Old.Path] = &modfile.Replace{Old: r.Old, New: module.Version{Path: newPath, Version: newVer}}
		}
	}

	driver.moduleRequirements[mod].replacements = replacements

	for _, req := range modFile.Require {
		if _, ok := replacements[req.Mod.Path]; !ok {
			if err := driver.determineVersionRequirements(req.Mod.Path, req.Mod.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveGetModules resolves the get wildcards with versions, and loads them into the driver. It returns the package
// parts of the get patterns e.g. github.com/example/module/...@v1.0.0 -> github.com/example/module/...
func (driver *pleaseDriver) resolveGetModules(patterns []string) ([]string, error) {
	pkgWildCards := make([]string, 0, len(patterns))
	for _, p := range patterns {
		progress.PrintUpdate("Resolving %v", p)

		parts := strings.Split(p, "@")
		pkgPart := parts[0]
		pkgWildCards = append(pkgWildCards, pkgPart)

		mod, err := driver.proxy.ResolveModuleForPackage(pkgPart)
		if err != nil {
			return nil, err
		}
		if len(parts) > 1 && strings.HasPrefix(parts[1], "v") {
			if err := driver.determineVersionRequirements(mod, parts[1]); err != nil {
				return nil, err
			}
		} else {
			ver, err := driver.proxy.GetLatestVersion(mod)
			if err != nil {
				return nil, err
			}
			if err := driver.determineVersionRequirements(mod, ver.Version); err != nil {
				return nil, err
			}
		}

	}
	return pkgWildCards, nil
}

// loadPleaseModules queries the Please build graph and loads in any modules defined there. It applies the minimum valid
// version algorithm.
func (driver *pleaseDriver) loadPleaseModules() error {
	out := &bytes.Buffer{}
	stdErr := &bytes.Buffer{}
	cmd := exec.Command(driver.pleaseTool, "query", "print", "-i", "go_module", "--json", fmt.Sprintf("//%s/...", driver.thirdPartyFolder))
	cmd.Stdout = out
	cmd.Stderr = stdErr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to query known modules: %v\n%v\n%v", err, out, stdErr)
	}

	res := map[string]struct {
		Outs   []string
		Labels []string
	}{}

	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		return err
	}

	for label, target := range res {
		rule := &goModDownloadRule{
			label:   label,
			srcRoot: filepath.Join("plz-out/gen", target.Outs[0]),
		}
		for _, l := range target.Labels {
			if strings.HasPrefix(l, "go_module:") {
				parts := strings.Split(strings.TrimPrefix(l, "go_module:"), "@")
				if len(parts) != 2 {
					return fmt.Errorf("invalid go_module label: %v", l)
				}

				// TODO: we probably need to recover the replace directives here somehow
				req := &requirement{
					mod: &packages.Module{Path: parts[0], Version: strings.TrimSpace(parts[1])},
				}
				oldMod, ok := driver.moduleRequirements[req.mod.Path]

				// Only add the Please version of this module if it's greater than or equal to the version requirement
				if !ok || semver.Compare(oldMod.mod.Version, req.mod.Version) <= 0 {
					driver.moduleRequirements[req.mod.Path] = req
					driver.pleaseModules[req.mod.Path] = rule
				}
			}
		}

	}
	return nil
}

// findPackageInKnownModules attempt to find the package in the existing modules to avoid hitting the proxy
func (driver *pleaseDriver) findPackageInKnownModules(id string) string {
	var candidate *packages.Module
	for _, req := range driver.moduleRequirements {
		if strings.HasPrefix(id, req.mod.Path) {
			if candidate == nil || len(candidate.Path) < len(req.mod.Path) {
				candidate = req.mod
			}
		}
	}
	if candidate == nil {
		return ""
	}

	root, err := driver.ensureDownloaded(candidate)
	if err != nil {
		return ""
	}

	pkgDir := strings.TrimPrefix(id, candidate.Path)
	if _, err := os.Lstat(filepath.Join(root, pkgDir)); err == nil {
		return candidate.Path
	}
	return ""
}

func (driver *pleaseDriver) ModuleForPackage(id string) (*requirement, error) {
	module := driver.findPackageInKnownModules(id)
	if module == "" {
		var err error
		module, err = driver.proxy.ResolveModuleForPackage(id)
		if err != nil {
			return nil, err
		}
	}

	if req, ok := driver.moduleRequirements[module]; ok {
		return req, nil
	}

	latest, err := driver.proxy.GetLatestVersion(module)
	if err != nil {
		return nil, err
	}

	// TODO(jpoole): this could cause updates of already downloaded modules. We probably need to re-run our analysis at
	//  this point
	if err := driver.determineVersionRequirements(module, latest.Version); err != nil {
		return nil, err
	}

	req, ok := driver.moduleRequirements[module]
	if !ok {
		return nil, fmt.Errorf("failed to determine module requirements for %v", id)
	}

	return req, nil
}
