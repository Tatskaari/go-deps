package proxy

import (
	"encoding/json"
	"fmt"
	"golang.org/x/mod/modfile"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

var client = http.DefaultClient

type ModuleNotFound struct {
	Path string
}

func (err ModuleNotFound) Error() string {
	return fmt.Sprintf("can't find module %v", err.Path)
}

// Module is the module and it's version returned from @latest
type Module struct {
	Module  string
	Version string
}

type Proxy struct {
	queryResults map[string]Module
	url          string
}

func New(url string) *Proxy {
	return &Proxy{
		queryResults: map[string]Module{},
		url:          url,
	}
}

// GetLatestVersion returns the latest version for a module from the proxy. Will return an error of type ModuleNotFound
// if no module exists for the given path
func (proxy *Proxy) GetLatestVersion(modulePath string) (Module, error) {
	if modulePath == "" {
		// TODO(jpoole): this shouldn't ever be hit but is hard to debug when it is. We can probably remove this once
		// 	the tool matures.
		panic("Must provide module path")
	}

	if result, ok := proxy.queryResults[modulePath]; ok {
		if result.Module != "" {
			return result, nil
		}
		return Module{}, ModuleNotFound{Path: modulePath}
	}

	resp, err := client.Get(fmt.Sprintf("%s/%s/@latest", proxy.url, strings.ToLower(modulePath)))
	if err != nil {
		return Module{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			proxy.queryResults[modulePath] = Module{}
			return Module{}, ModuleNotFound{Path: modulePath}
		}
		return Module{}, fmt.Errorf("unexpected status code getting module %v: %v", modulePath, resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Module{}, err
	}

	version := struct {
		Version string
	}{}
	if err := json.Unmarshal(b, &version); err != nil {
		return Module{}, err
	}

	proxy.queryResults[modulePath] = Module{
		Module:  modulePath,
		Version: version.Version,
	}
	return proxy.queryResults[modulePath], nil
}

// ResolveModuleForPackage tries to determine the module name for a given package pattern
func (proxy *Proxy) ResolveModuleForPackage(pattern string) (string, error) {
	modulePath := strings.TrimSuffix(pattern, "/...")

	var paths []string
	for modulePath != "." {
		paths = append(paths, modulePath)
		// Try and get the latest version to determine if we've found the module part yet
		latest, err := proxy.GetLatestVersion(modulePath)
		if err == nil {
			for _, p := range paths {
				proxy.queryResults[p] = latest
			}
			return latest.Module, nil
		}
		if _, ok := err.(ModuleNotFound); !ok {
			return "", err
		}

		modulePath = filepath.Dir(modulePath)
	}
	return "", fmt.Errorf("couldn't find module for package %v", pattern)
}

func (proxy *Proxy) GetGoMod(mod, ver string) (*modfile.File, error) {
	// TODO(jpoole): save this to disk to avoid downloading it each time
	file := fmt.Sprintf("%s/%s/@v/%s.mod", proxy.url, strings.ToLower(mod), ver)
	resp, err := client.Get(file)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%v %v: \n%v", file, resp.StatusCode, string(body))
	}

	return modfile.Parse(file, body, nil)
}
