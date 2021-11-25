subinclude("//:version")

def cross_compile(arch):
    return build_rule(
        name = f"go_deps_{arch}",
        srcs = [f"///{arch}//:go-deps"],
        outs = [f"go_deps_v{VERSION}_{arch}"],
        cmd = "mv $SRC $OUT",
    )

filegroup(
    name = "release_files",
    srcs = [cross_compile(arch) for arch in [
        "darwin_amd64",
        "darwin_arm64",
        "linux_amd64",
    ]],
    labels = ["hlink:plz-out/package"],
)
