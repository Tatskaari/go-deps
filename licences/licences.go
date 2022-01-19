package licences

import (
	"fmt"
	"github.com/google/go-licenses/licenses"
	"github.com/tatskaari/go-deps/resolve"
	"github.com/tatskaari/go-deps/resolve/driver"
	"github.com/tatskaari/go-deps/resolve/model"
	"golang.org/x/tools/go/packages"
	"os"
	"path/filepath"
)

func SetLicences(modules *resolve.Modules, driver *driver.PleaseDriver) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	var paths = map[string]*model.Module{}
	for _, m := range modules.Mods {
		for _, p := range m.Parts {
			p.Modified = true
		} // So the licences actually get set
		root, err := driver.EnsureDownloaded(&packages.Module{Path: m.Name, Version: m.Version})
		if err != nil {
			continue
		}

		paths[root] = m
	}

	c, _ := licenses.NewClassifier(0.9)

	for root, m := range modules.Mods {
		licence, _, err := c.Identify(filepath.Join(wd, "plz-out/godeps/go", root))
		if err != nil {
			return fmt.Errorf("failed to identify licence ofr %s: %v", root, err)
		}
		m.Licence = licence
	}
	return nil
}
