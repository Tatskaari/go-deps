package licences

import (
	"github.com/tatskaari/go-deps/resolve"
	"golang.org/x/tools/go/packages"
)

func SetLicences(modules *resolve.Modules, driver packages.Driver) error {
	var paths []string
	for _, m := range modules.Mods {
		m.Parts[0].Modified = true // So the licences actually get set
		paths = append(paths, m.Name)
	}

	pkgs, r, err := resolve.Load(paths, driver)
	if err != nil {
		return err
	}

	return r.SetLicence(pkgs)
}
