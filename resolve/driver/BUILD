go_library(
    name = "driver",
    srcs = [
        "module.go",
        "please_driver.go",
    ],
    visibility = ["PUBLIC"],
    deps = [
        "//progress",
        "//resolve/knownimports",
        "//resolve/driver/proxy",
        "//third_party/go/golang.org/x/mod",
        "//third_party/go/golang.org/x/net",
        "//third_party/go/golang.org/x/tools",
    ],
)
