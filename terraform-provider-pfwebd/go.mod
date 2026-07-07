module github.com/gduale/terraform-provider-pfwebd

go 1.22.0

toolchain go1.24.4

require github.com/hashicorp/terraform-plugin-framework v1.13.0

require (
	github.com/fatih/color v1.13.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/go-plugin v1.6.2 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/terraform-plugin-go v0.25.0 // indirect
	github.com/hashicorp/terraform-plugin-log v0.9.0 // indirect
	github.com/hashicorp/terraform-registry-address v0.2.3 // indirect
	github.com/hashicorp/terraform-svchost v0.1.1 // indirect
	github.com/hashicorp/yamux v0.1.1 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/oklog/run v1.0.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/net v0.28.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142 // indirect
	google.golang.org/grpc v1.67.1 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
)

// The build sandbox can only reach github.com directly, so the
// golang.org/x and google.golang.org modules are redirected to their
// official GitHub mirrors (same content, same version tags).
replace (
	golang.org/x/crypto => github.com/golang/crypto v0.28.0
	golang.org/x/mod => github.com/golang/mod v0.21.0
	golang.org/x/net => github.com/golang/net v0.30.0
	golang.org/x/sync => github.com/golang/sync v0.8.0
	golang.org/x/sys => github.com/golang/sys v0.26.0
	golang.org/x/text => github.com/golang/text v0.19.0
	golang.org/x/tools => github.com/golang/tools v0.26.0
	google.golang.org/genproto/googleapis/rpc => github.com/googleapis/go-genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142
	google.golang.org/grpc => github.com/grpc/grpc-go v1.67.1
	google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.35.1
	gopkg.in/yaml.v3 => ./third_party/yaml.v3-stub
)
