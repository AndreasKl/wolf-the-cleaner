// Package e2e contains Dockerized end-to-end tests for the wolfe binary. The
// tests are guarded by the `e2e` build tag so they do not run during a normal
// `go test ./...`; run them via e2e/run.sh (docker build + docker run).
package e2e
