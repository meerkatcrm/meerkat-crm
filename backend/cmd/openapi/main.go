// Command openapi writes the OpenAPI document of the huma operations to the file
// named by its argument, or to stdout. Run it with `make openapi` after changing
// an operation; routes.TestOpenAPISpecIsCurrent fails until you do.
package main

import (
	"fmt"
	"os"

	"meerkat/routes"
)

func main() {
	doc, err := routes.OpenAPISpec()
	if err == nil && len(os.Args) > 1 {
		err = os.WriteFile(os.Args[1], doc, 0o644)
	} else if err == nil {
		_, err = os.Stdout.Write(doc)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
}
