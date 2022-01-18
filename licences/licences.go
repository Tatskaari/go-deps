package licences

import (
	"fmt"
	"github.com/tatskaari/go-deps/resolve"
	"golang.org/x/tools/go/packages"
)

func SetLicences(modules *resolve.Modules, driver packages.Driver) error {
	var paths []string
	for _, m := range modules.Mods {
		for _, p := range m.Parts {
			p.Modified = true
		} // So the licences actually get set
		paths = append(paths, fmt.Sprintf("%v@%v", m.Name, m.Version))
	}

	pkgs, r, err := resolve.Load(paths, driver)
	if err != nil {
		return err
	}

	return r.SetLicence(pkgs)
}
