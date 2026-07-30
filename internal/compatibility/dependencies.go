package compatibility

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// DependencyNode is the production import information needed by the architecture gate.
type DependencyNode struct {
	Path     string
	Standard bool
	Imports  []string
}

// DependencyViolation identifies a forbidden dependency path from a checked root.
type DependencyViolation struct {
	Rule       string
	Root       string
	Dependency string
}

// LoadDependencyGraph loads non-test packages and all dependencies for the supplied patterns.
func LoadDependencyGraph(dir string, patterns ...string) (map[string]DependencyNode, []string, error) {
	config := &packages.Config{
		Dir:   dir,
		Mode:  packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		Tests: false,
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, nil, err
	}
	var packageErrors []string
	seen := make(map[string]bool)
	graph := make(map[string]DependencyNode)
	var visit func(*packages.Package)
	visit = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		for _, packageError := range pkg.Errors {
			packageErrors = append(packageErrors, packageError.Error())
		}
		imports := make([]string, 0, len(pkg.Imports))
		for path, imported := range pkg.Imports {
			imports = append(imports, path)
			visit(imported)
		}
		sort.Strings(imports)
		graph[pkg.PkgPath] = DependencyNode{Path: pkg.PkgPath, Standard: pkg.Module == nil, Imports: imports}
	}
	for _, pkg := range loaded {
		visit(pkg)
	}
	sort.Strings(packageErrors)
	return graph, packageErrors, nil
}

// ProductionRoots returns module packages that may ship in production binaries.
func ProductionRoots(modulePath string, graph map[string]DependencyNode) []string {
	prefix := modulePath + "/"
	var roots []string
	for path := range graph {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		relative := strings.TrimPrefix(path, prefix)
		if relative == "testkit" || strings.HasPrefix(relative, "testkit/") ||
			strings.HasPrefix(relative, "internal/") || strings.HasPrefix(relative, "compat/") {
			continue
		}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	return roots
}

// CheckDependencies applies the framework architecture rules to a dependency graph.
func CheckDependencies(modulePath string, graph map[string]DependencyNode, productionRoots []string) []DependencyViolation {
	testkitPath := modulePath + "/testkit"
	postgresPath := modulePath + "/postgres"
	outboxPath := postgresPath + "/outbox"
	inboxPath := postgresPath + "/inbox"
	var violations []DependencyViolation
	add := func(rule, root, dependency string) {
		violations = append(violations, DependencyViolation{Rule: rule, Root: root, Dependency: dependency})
	}

	for _, root := range productionRoots {
		for dependency := range dependencyClosure(graph, root) {
			if dependency == testkitPath || strings.HasPrefix(dependency, testkitPath+"/") {
				add("production-no-testkit", root, dependency)
			}
		}
	}

	for _, relative := range []string{"domain", "repository", "eventing", "application"} {
		root := modulePath + "/" + relative
		for dependency := range dependencyClosure(graph, root) {
			if dependency == postgresPath || strings.HasPrefix(dependency, postgresPath+"/") {
				add("core-no-postgres", root, dependency)
			}
		}
	}
	for dependency := range dependencyClosure(graph, outboxPath) {
		if dependency == inboxPath || strings.HasPrefix(dependency, inboxPath+"/") {
			add("outbox-inbox-isolation", outboxPath, dependency)
		}
	}
	for dependency := range dependencyClosure(graph, inboxPath) {
		if dependency == outboxPath || strings.HasPrefix(dependency, outboxPath+"/") {
			add("outbox-inbox-isolation", inboxPath, dependency)
		}
	}

	allowedTestkitPackages := map[string]bool{
		modulePath + "/application": true,
		modulePath + "/domain":      true,
		modulePath + "/eventing":    true,
		modulePath + "/repository":  true,
	}
	for dependency := range dependencyClosure(graph, testkitPath) {
		node, ok := graph[dependency]
		if ok && node.Standard {
			continue
		}
		if allowedTestkitPackages[dependency] {
			continue
		}
		add("testkit-core-only", testkitPath, dependency)
	}

	sort.Slice(violations, func(i, j int) bool {
		left := fmt.Sprintf("%s\x00%s\x00%s", violations[i].Rule, violations[i].Root, violations[i].Dependency)
		right := fmt.Sprintf("%s\x00%s\x00%s", violations[j].Rule, violations[j].Root, violations[j].Dependency)
		return left < right
	})
	return violations
}

func dependencyClosure(graph map[string]DependencyNode, root string) map[string]bool {
	result := make(map[string]bool)
	seen := map[string]bool{root: true}
	var visit func(string)
	visit = func(path string) {
		node, ok := graph[path]
		if !ok {
			return
		}
		for _, dependency := range node.Imports {
			if seen[dependency] {
				continue
			}
			seen[dependency] = true
			result[dependency] = true
			visit(dependency)
		}
	}
	visit(root)
	return result
}
