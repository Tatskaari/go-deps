go_library(
    name = "resolve",
    srcs = ["resolve.go"],
    visibility = ["PUBLIC"],
    deps = [
        "//progress",
        "//resolve/driver",
        "//resolve/knownimports",
        "//resolve/model",
        "//third_party/go/github.com/google/go-licenses",
        "//third_party/go/golang.org/x/mod",
        "//third_party/go/golang.org/x/tools",
    ],
)

go_test(
    name = "resolve_test",
    srcs = ["resolve_test.go"],
    deps = [
        ":resolve",
        "//third_party/go/github.com/stretchr/testify",
    ],
)
