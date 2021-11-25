# Go-Deps

This tool is used to help maintain your `go_module()` rules in a [Please](https://please.build) project.

# Features

Go-deps can be used to update existing modules to newer version, or add new modules to your project. It 
works by parsing your existing BUILD files in your third party folder (by default `third_party/go/BUILD`, 
and updating them non-destructively. 

Go-deps has two modes of operation. It can generate a flat BUILD file, e.g. `third_party/go/BUILD`, or it
can split out each module into its own build file e.g. `third_party/go/github.com/example/module/BUILD`.
That later can be very useful to improve maintainability in larger mono-repos, especially if you use `OWNER`
files to assign reviewers to branches of the source tree. 

# Installation

The simplest way to use this tool is to add the following to your project:

```
GO_DEPS_VERSION = < version here, check https://github.com/Tatskaari/go-deps/releases >

remote_file(
    name = "go-deps",
    binary = True,
    url = f"https://github.com/Tatskaari/go-deps/releases/download/{GO_DEPS_VERSION}/go-deps",
)
```

This can then be ran with `plz run //tools:go-deps -- -w github.com/example/module/...@v1.0.0`, where `//tools` is the 
package you added the above rules to.

## Aliases
To avoid specifying the target each time, an alias can be used. Add the following to your `.plzconfig`:
```
[alias "go-get"]
desc = Runs the go deps tool to install new dependencies into the repo
cmd = run //tools:go-deps -- 
```

Which can then be invoked as such:
```
$ plz go-get -w example.com/some/module
```

# Usage
Simply run `go-deps -w github.com/example/module/...`. Use the `go get` style `package@version` syntax to specify a 
specific version, e.g. `go-deps -w github.com/example/module/...@v1.0.0`. This tool will install the latest version by 
default.

N.B: This tool operates on packages, not modules. Make sure the package you target actually contains `.go` files. Use
the `...` wildcard to install all packages under a certain path, as in the example above. 

To add the `go_module()` rules into separate `BUILD` files for each module, pass the `--structured, -s` flag.

```
Example usage: 
  go-deps -w github.com/example/module/...@v1.0.0

Usage:
  go-deps [OPTIONS] [packages...]

Application Options:
      --third_party= The location of the folder containing your third party build rules. (default: third_party/go)
  -s, --structured   Whether to produce a structured directory tree for each module. Defaults to a flat BUILD file for all third party rules.
  -w, --write        Whether write the rules back to the BUILD files. Prints to stdout by default.
      --please_path= The path to the Please binary. (default: plz)

Help Options:
  -h, --help         Show this help message

Arguments:
  packages:          Packages to install following 'go get' style patters. These can optionally have versions e.g. github.com/example/module/...@v1.0.0
```

