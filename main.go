package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gstruct/terraform-provider-run/provider"
	terraform "github.com/hashicorp/terraform/plugin"
)

func main() {
	log.SetFlags(log.Lshortfile)
	log.SetPrefix(fmt.Sprintf("pid-%d-", os.Getpid()))

	terraform.Serve(&terraform.ServeOpts{
		ProviderFunc: provider.Provider,
	})
}
