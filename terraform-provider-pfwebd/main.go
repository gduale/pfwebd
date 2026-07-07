// terraform-provider-pfwebd manages an OpenBSD PF firewall through the
// pfwebd REST API (anchor ruleset and PF tables).
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/gduale/terraform-provider-pfwebd/internal/provider"
)

// version is set by the build process (goreleaser/ldflags).
var version = "0.1.0"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with debug support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/gduale/pfwebd",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
