package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.idp.xyz/idp-bento-go/internal/compatibility"
)

const modulePath = "go.idp.xyz/idp-bento-go"

func main() {
	if err := run(".", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dependencycheck:", err)
		os.Exit(1)
	}
}

func run(dir string, stdout io.Writer) error {
	graph, packageErrors, err := compatibility.LoadDependencyGraph(dir, "./...")
	if err != nil {
		return err
	}
	if len(packageErrors) > 0 {
		return fmt.Errorf("load production graph: %s", strings.Join(packageErrors, "; "))
	}
	roots := compatibility.ProductionRoots(modulePath, graph)
	if len(roots) == 0 {
		return errors.New("production graph contains no framework packages")
	}
	violations := compatibility.CheckDependencies(modulePath, graph, roots)
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintf(stdout, "[%s] %s -> %s\n", violation.Rule, violation.Root, violation.Dependency)
		}
		return fmt.Errorf("production dependency graph has %d violation(s)", len(violations))
	}
	fmt.Fprintf(stdout, "dependencycheck: %d production packages are clean\n", len(roots))
	return nil
}
