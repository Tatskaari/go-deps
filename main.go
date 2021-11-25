package main

import (
	"io/fs"
	"log"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/tatskaari/go-deps/resolve"
	"github.com/tatskaari/go-deps/resolve/driver"
	"github.com/tatskaari/go-deps/rules"
)

var opts struct {
	ThirdPartyFolder string `long:"third_party" default:"third_party/go" description:"The location of the folder containing your third party build rules."`
	Structured       bool   `long:"structured" short:"s" description:"Whether to produce a structured directory tree for each module. By default, a flat BUILD file for all third party rules."`
	Write            bool   `long:"write" short:"w" description:"Whether write the rules back to the BUILD files. Prints to stdout by default."`
	PleasePath       string `long:"please_path" default:"plz" desciption:"The path to the Please binary."`
	Args             struct {
		Packages []string `positional-arg-name:"packages" description:"Packages to install following 'go get' style patters. These can optionally have versions e.g. github.com/example/module...@v1.0.0"`
	} `positional-args:"true"`
}

// This binary will accept a module name and optionally a semver or commit hash, and will add this module to a BUILD file.
func main() {
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		log.Fatal(err)
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

	err := resolve.UpdateModules(moduleGraph.Modules, opts.Args.Packages, driver.NewPleaseDriver(opts.PleasePath, opts.ThirdPartyFolder))
	if err != nil {
		log.Fatal(err)
	}

	if err := moduleGraph.Format(opts.Structured, opts.Write, opts.ThirdPartyFolder); err != nil {
		log.Fatal(err)
	}
}
