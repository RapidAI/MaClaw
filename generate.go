// Package codeclaw anchors repository-root go:generate directives.
//
// It contains no runtime code. Its sole purpose is to host the go:generate
// directive below so that running `go generate ./...` from the repository root
// regenerates code-generated artifacts whose generators expect the repository
// root as their working directory.
//
// The phase-metadata generator (cmd/genphasemeta) resolves the package path
// `./cmd/genphasemeta` and writes its output to the repo-root-relative path
// `gui/frontend/src/components/ai/workflowPhaseMeta.generated.ts`. Both only
// resolve correctly when the directive's working directory is the repository
// root, which is exactly the directory of this file.
package codeclaw

//go:generate go run ./cmd/genphasemeta
