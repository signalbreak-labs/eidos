package main

import (
	"context"
	"flag"
	"fmt"
	"log"
)
import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5/tf5server"
	provider "github.com/mycloud/terraform-provider-mycloud/internal/provider"
)

// main is the executable entry point for the generated Terraform provider.
var (
	version string = "dev"
	commit  string = "none"
	date    string = "unknown"
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Set to true to run the provider with support for debuggers like delve")
	var protocolVersion int
	flag.IntVar(&protocolVersion, "protocol-version", 6, "Terraform plugin protocol version to serve (5 or 6)")
	var printVersion bool
	flag.BoolVar(&printVersion, "version", false, "Print version information and exit")
	flag.Parse()
	if printVersion {
		fmt.Printf("version=%s\ncommit=%s\ndate=%s\n", version, commit, date)
		return
	}
	address := "registry.terraform.io/mycloud/mycloud"
	if protocolVersion == 5 {
		err := tf5server.Serve(address, providerserver.NewProtocol5(provider.New()))
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if protocolVersion != 6 {
		log.Printf("warning: unsupported --protocol-version %d; defaulting to protocol version 6", protocolVersion)
	}
	opts := providerserver.ServeOpts{Address: address, Debug: debug}
	err := providerserver.Serve(context.Background(), provider.New, opts)
	if err != nil {
		log.Fatal(err)
	}
}
