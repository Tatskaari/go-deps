package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/tatskaari/go-deps/resolve"
	"github.com/tatskaari/go-deps/resolve/driver"
	"github.com/tatskaari/go-deps/rules"
)

var opts struct {
	ThirdPartyFolder string `long:"third_party" default:"third_party/go" description:"The location of the folder containing your third party build rules."`
	Structured       bool   `long:"structured" short:"s" description:"Whether to produce a structured directory tree for each module. Defaults to a flat BUILD file for all third party rules."`
	Write            bool   `long:"write" short:"w" description:"Whether write the rules back to the BUILD files. Prints to stdout by default."`
	PleaseTool       string `long:"please_tool" default:"plz" description:"The path to the Please binary."`
	GoTool           string `long:"go_tool" default:"go" description:"The path to the Please binary."`
	BuildFileName    string `long:"build_file_name" default:"BUILD" description:"The filename to use for BUILD files. Defaults to BUILD."`
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

	moduleGraph := rules.NewGraph(opts.BuildFileName, opts.Structured, opts.ThirdPartyFolder)
	if opts.Structured {
		err := filepath.Walk(opts.ThirdPartyFolder, func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			if filepath.Base(path) == opts.BuildFileName {
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
		if err := moduleGraph.ReadRules(filepath.Join(opts.ThirdPartyFolder, opts.BuildFileName)); err != nil {
			log.Fatal(err)
		}
	}

	err := resolve.UpdateModules(opts.GoTool, moduleGraph.Modules, opts.Args.Packages, driver.NewPleaseDriver(opts.PleaseTool, opts.GoTool, opts.ThirdPartyFolder))
	if err != nil {
		log.Fatal(err)
	}

	if err := moduleGraph.Format(opts.Write); err != nil {
		log.Fatal(err)
	}
}
