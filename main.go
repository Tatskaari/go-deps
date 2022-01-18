package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/tatskaari/go-deps/licences"
	"github.com/tatskaari/go-deps/resolve"
	"github.com/tatskaari/go-deps/resolve/driver"
	"github.com/tatskaari/go-deps/rules"
)

var opts struct {
	ThirdPartyFolder string `long:"third_party" default:"third_party/go" description:"The location of the folder containing your third party build rules."`
	Structured       bool   `long:"structured" short:"s" description:"Whether to produce a structured directory tree for each module. Defaults to a flat BUILD file for all third party rules."`
	Write            bool   `long:"write" short:"w" description:"Whether write the rules back to the BUILD files. Prints to stdout by default."`
	PleasePath       string `long:"please_path" default:"plz" description:"The path to the Please binary."`
	LicencesOnly     bool   `long:"update_licences_only" description:"If set, the tool will update all licences for modules in the third party folder"`
	Args             struct {
		Packages []string `positional-arg-name:"packages" description:"Packages to install following 'go get' style patters. These can optionally have versions e.g. github.com/example/module/...@v1.0.0"`
	} `positional-args:"true"`
}

// This binary will accept a module name and optionally a semver or commit hash, and will add this module to a BUILD file.
func main() {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	if _, err := parser.Parse(); err != nil {
		fmt.Fprintf(os.Stderr, "Godeps is a developer productivity tool for the Please build system.\n"+
			"It can add and updates third party modules to your project through \nan interface that should feel familiar to those used to `go get`.\n\n"+
			"Example usage: \n"+
			"  go-deps -w github.com/example/module/...@v1.0.0\n\n")
		fmt.Fprintf(os.Stderr, "%v", err)
		os.Exit(1)
	}

	// TODO(jpoole): load the BuildFileName from the .plzconfig
	moduleGraph := rules.NewGraph()
	if opts.Structured {
		err := filepath.Walk(opts.ThirdPartyFolder, func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			if filepath.Base(path) == "BUILD" {
				if err := moduleGraph.ReadRules(path); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	} else {
		if err := moduleGraph.ReadRules(filepath.Join(opts.ThirdPartyFolder, "BUILD")); err != nil {
			log.Fatal(err)
		}
	}

	if !opts.LicencesOnly {
		err := resolve.UpdateModules(moduleGraph.Modules, opts.Args.Packages, driver.NewPleaseDriver(opts.PleasePath, opts.ThirdPartyFolder))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		err := licences.SetLicences(moduleGraph.Modules, driver.NewPleaseDriver(opts.PleasePath, opts.ThirdPartyFolder))
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := moduleGraph.Format(opts.Structured, opts.Write, opts.ThirdPartyFolder); err != nil {
		log.Fatal(err)
	}
}
