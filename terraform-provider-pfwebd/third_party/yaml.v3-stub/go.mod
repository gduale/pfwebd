// Stub for gopkg.in/yaml.v3, whose host is unreachable from the build
// sandbox. This module is only referenced by test dependencies of
// indirect dependencies (go-hclog -> testify -> yaml.v3); none of its
// packages are ever imported or built by this provider.
module gopkg.in/yaml.v3

go 1.20
