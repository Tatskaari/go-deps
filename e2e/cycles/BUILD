subinclude("//e2e/build_defs:please_repo")

please_repo_e2e_test(
    name = "cycles_test",
    get_wildcards = [
        "github.com/googleapis/gax-go/v2/...",
        "google.golang.org/protobuf/... google.golang.org/grpc/...",
    ],
    plz_command = "plz test",
    repo = "test_repo",
)
