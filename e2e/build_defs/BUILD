filegroup(
    name="please_repo",
    srcs = ["please_repo.build_defs"],
    visibility = ["PUBLIC"],
)

filegroup(
    name = "base_config",
    srcs = [
        ".plzconfig.e2e",
    ],
    visibility = ["PUBLIC"],
)

remote_file(
    name = "please",
    url = f"https://get.please.build/{CONFIG.OS}_{CONFIG.ARCH}/{CONFIG.PLZ_VERSION}/please_{CONFIG.PLZ_VERSION}",
    binary = True,
    test_only = True,
    visibility = ["PUBLIC"],
)