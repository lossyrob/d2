# Copilot Instructions for D2

D2 is a diagram scripting language that compiles text into diagrams. Module path: `oss.terrastruct.com/d2`.

## Build & Test

```sh
git submodule update --init --recursive   # required after clone
./make.sh          # fmt + gen + lint + build + test (full CI)
./make.sh build    # build only
./make.sh test     # all tests
./make.sh lint     # go vet
./make.sh race     # tests with -race
```

Run a single test:

```sh
go test -run TestCompile/basic_shape ./d2compiler/...
```

### Golden file testing

Most tests compare output against checked-in golden files in `testdata/` directories using `diff.Testdata()` and `diff.TestdataJSON()` from `oss.terrastruct.com/util-go/diff`. A new test will fail on its first run because no golden file exists yet. Accept new/updated output with:

```sh
TESTDATA_ACCEPT=1 go test -run TestMyNew ./package/...
```

Visual diff for e2e tests:

```sh
./ci/e2ereport.sh -delta              # HTML report of all changes
./ci/e2ereport.sh -delta -run TestFoo # single test delta
```

### Chaos tests

```sh
D2_CHAOS_MAXI=100 D2_CHAOS_N=100 ./ci/test.sh ./d2chaos
```

## Architecture

The compilation pipeline passes these key types between stages:

```
d2parser.Parse  →  d2ast.Map
d2ir.Compile    →  d2ir.Map      (semantic analysis: imports, globs, field merging)
d2compiler.Compile → d2graph.Graph (objects, edges, attributes with positions)
d2layouts.LayoutNested            (applies dagre/elk layout engine)
d2exporter.Export → d2target.Diagram
d2svg.Render / d2ascii.Render    (final output)
```

Key packages:

- **d2ast** — AST node types (`Map`, `Key`, `Edge`, scalars). Every node carries a `Range` for error reporting.
- **d2ir** — Intermediate representation. Resolves imports, globs, and field/edge merging before compilation.
- **d2graph** — Core data model: `Graph` (root `Object`, flat `Objects`/`Edges` lists, `Layers`/`Scenarios`/`Steps` for multi-board). `SerializeGraph`/`DeserializeGraph` for JSON roundtrips.
- **d2layouts** — `LayoutNested()` recursively extracts subgraphs (grids, sequences, constant-near) and delegates to layout plugins.
- **d2plugin** — `Plugin` interface (`Layout`, `PostProcess`) and `RoutingPlugin` interface. Built-in: dagre, ELK. External plugins discovered as `d2plugin-*` binaries in PATH.
- **d2oracle** — Query/edit utilities for navigating and manipulating graph+AST (used by LSP and programmatic editing).
- **d2lib** — High-level API that wires the full pipeline together. Entry point for library usage.
- **d2cli** — CLI entry point. `main.go` calls `xmain.Main(d2cli.Run)`.

## Conventions

- **Test packages are external**: `package d2compiler_test`, not `package d2compiler`. Tests import the package under test.
- **Test cases use `t.Parallel()`**.
- **E2E tests** (`e2etests/`) run each case against both dagre and elk layout engines unless `justDagre: true`.
- **Error types** carry source positions: `d2ast.Error` has `Range` + `Message`. Parser and compiler errors include file path and line/column.
- **PR titles** use a scope prefix: `cli: performance improvements`, `compiler: fix glob resolution`.
- **PRs must update** `ci/release/changelogs/next.md`, the manpage, and CLI help docs when applicable.
- **Code generation**: `./make.sh gen` runs `ci/gen.sh`. Commit generated output; CI asserts the tree is clean.
- **Imports** use the full module path: `"oss.terrastruct.com/d2/d2graph"`, `"oss.terrastruct.com/util-go/assert"`.

## Visual QA Process for Layout Development

When developing or iterating on layout engines, use this render→inspect→fix loop:

1. **Render**: Build the binary (`go build -o /tmp/d2-test .`) and render a `.d2` file to PNG with the target layout engine.
2. **Inspect**: Use the `view` tool to visually inspect the PNG. Check node placement, edge routing, label positioning, overlaps, and overall aesthetic quality.
3. **Fix**: Make code changes to address issues found during inspection.
4. **Copy to repo**: After each render, copy the current PNG into the repo root (e.g., `diagram1-widescreen-current.png`) so progress can be monitored externally.
5. **Loop**: Repeat until the diagram looks correct and high quality. Do NOT settle for "good enough" — persist with new techniques, algorithms, and approaches until the output is genuinely excellent.

Run this loop for at least 5 test diagrams, each covering different edge cases (varying node counts, nesting depths, edge patterns, cross-boundary connections). For new test diagrams, first validate they render correctly with an established layout engine (e.g., ELK) before testing with the new engine.

**Quality bar**: Edge routing must be clean and readable. Labels must not overlap. Nodes must be well-spaced. The overall diagram must look like it was designed by a human, not auto-generated.
