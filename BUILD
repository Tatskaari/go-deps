genrule(
    name = "version",
    srcs = ["VERSION"],
    outs = ["version.build_defs"],
    cmd = "echo VERSION = \\\"$(cat $SRCS)\\\" > $OUT",
    visibility = ["PUBLIC"],
)

go_binary(
    name = "go-deps",
    srcs = ["main.go"],
    visibility = ["PUBLIC"],
    deps = [
        "//resolve",
        "//licences",
        "//resolve/driver",
        "//rules",
        "//third_party/go/github.com/jessevdk/go-flags",
    ],
)
