package compatibility

import (
	"path/filepath"
	"testing"
)

const testModulePath = "go.idp.xyz/idp-bento-go"

func TestDependencyNegativeFixtures(t *testing.T) {
	t.Parallel()
	testkit := testModulePath + "/testkit"
	postgres := testModulePath + "/postgres"
	outbox := postgres + "/outbox"
	inbox := postgres + "/inbox"
	thirdParty := "go.opentelemetry.io/otel/sdk"
	fixtures := []struct {
		name       string
		graph      map[string]DependencyNode
		roots      []string
		wantRule   string
		wantRoot   string
		dependency string
	}{
		{
			name: "production imports testkit",
			graph: map[string]DependencyNode{
				testModulePath + "/shipping": {Path: testModulePath + "/shipping", Imports: []string{testkit}},
				testkit:                      {Path: testkit},
			},
			roots:      []string{testModulePath + "/shipping"},
			wantRule:   "production-no-testkit",
			wantRoot:   testModulePath + "/shipping",
			dependency: testkit,
		},
		{
			name: "core imports postgres",
			graph: map[string]DependencyNode{
				testModulePath + "/domain": {Path: testModulePath + "/domain", Imports: []string{postgres}},
				postgres:                   {Path: postgres},
			},
			wantRule:   "core-no-postgres",
			wantRoot:   testModulePath + "/domain",
			dependency: postgres,
		},
		{
			name: "outbox imports inbox",
			graph: map[string]DependencyNode{
				outbox: {Path: outbox, Imports: []string{inbox}},
				inbox:  {Path: inbox},
			},
			wantRule:   "outbox-inbox-isolation",
			wantRoot:   outbox,
			dependency: inbox,
		},
		{
			name: "inbox imports outbox",
			graph: map[string]DependencyNode{
				inbox:  {Path: inbox, Imports: []string{outbox}},
				outbox: {Path: outbox},
			},
			wantRule:   "outbox-inbox-isolation",
			wantRoot:   inbox,
			dependency: outbox,
		},
		{
			name: "testkit imports infrastructure",
			graph: map[string]DependencyNode{
				testkit:    {Path: testkit, Imports: []string{thirdParty}},
				thirdParty: {Path: thirdParty},
			},
			wantRule:   "testkit-core-only",
			wantRoot:   testkit,
			dependency: thirdParty,
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			violations := CheckDependencies(testModulePath, fixture.graph, fixture.roots)
			assertDependencyViolation(t, violations, fixture.wantRule, fixture.wantRoot, fixture.dependency)
		})
	}
}

func TestRealTestkitImportFixtureIsRejected(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repositoryRoot(t), "internal", "compatibility", "testdata", "testkitleak")
	graph, packageErrors, err := LoadDependencyGraph(dir, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(packageErrors) > 0 {
		t.Fatalf("load fixture: %v", packageErrors)
	}
	fixturePath := testModulePath + "/internal/compatibility/testdata/testkitleak"
	violations := CheckDependencies(testModulePath, graph, []string{fixturePath})
	assertDependencyViolation(t, violations, "production-no-testkit", fixturePath, testModulePath+"/testkit")
}

func assertDependencyViolation(t *testing.T, violations []DependencyViolation, rule, root, dependency string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Rule == rule && violation.Root == root && violation.Dependency == dependency {
			return
		}
	}
	t.Fatalf("missing violation %s %s -> %s in %#v", rule, root, dependency, violations)
}
