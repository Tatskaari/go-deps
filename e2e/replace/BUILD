subinclude("//e2e/build_defs:please_repo")

please_repo_e2e_test(
    name = "replace_test",
    plz_command = "mkdir -p third_party/go && $TOOLS_GO_DEPS --build_file_name BUILD_FILE --please_tool $TOOLS_PLEASE --go_tool $TOOLS_GO -w github.com/aws/amazon-ssm-agent/... && plz test -o go.gotool:$TOOLS_GO",
    repo = "test_repo",
    tools = {
        "go_deps": ["//:go-deps"],
        "go": ["//third_party/go:toolchain|go"],
    }
)
