# SenseGuard verification and deployment report

Date: 2026-10-19

## Decision

**A. NOT READY FOR STAGING.** Current-source checks and an isolated local runtime smoke test pass, but production identity issuance, real certificate provisioning, Kubernetes schema/cluster validation, persistent-volume behavior, and a target-environment recovery exercise are outstanding. Use simulated data only. No staging, production, cloud, or Kubernetes deployment was performed.

## Architecture and controls

SenseGuard is a Go HTTP API that authenticates consumers using startup-configured bearer tokens, asks OPA to authorize purpose and privacy mode, transforms simulated sensing data, and charges a per-consumer SQLite ledger before returning a response. `/budget` uses the same authenticated identity as `/sensing`; `X-Consumer-ID` cannot select another account. OPA and database failures fail closed. The ledger is a single-instance design.

Compose is a localhost-only development profile. API-to-OPA traffic is HTTP on its private Compose network, and the OPA API does not authenticate Compose clients. Its health check evaluates an expected policy decision, confirms the authorization policy is loaded, and checks OPA's loopback diagnostic endpoint. SenseGuard readiness checks SQLite and OPA. Containers use non-root execution, read-only root filesystems, dropped capabilities, no-new-privileges, and resource limits. `.dockerignore` limits the build context to the Dockerfile, Go module files, and application source.

The Kubernetes manifests are preparation only. They configure API HTTPS and mTLS to OPA; OPA pins the client certificate subject to `CN=senseguard-api` and limits API access to the policy decision and health endpoints. NetworkPolicy enforcement depends on the cluster CNI. The Service is internal, no Ingress is configured, and the SQLite PVC is not a backup.

The README includes request-flow and deployment-topology diagrams and illustrative privacy-mode images. They describe demo behavior and do not claim a formal privacy guarantee.

## Checks on the current source

All checks below completed on 2026-10-19. Go and OPA ran from versioned containers because host binaries are not installed. The repository was mounted read-only for Go tests; OPA policy files were mounted read-only.

| Check | Result |
|---|---|
| `go test ./...` with Go 1.24.13 | PASS, application package and one ignored historical audit copy |
| `go test ./cmd/senseguard/...` with Go 1.24.13 | PASS |
| `go vet ./cmd/senseguard/...` with Go 1.24.13 | PASS, no diagnostics |
| `go test -race ./cmd/senseguard/...` with Go 1.24.13 | PASS |
| `go test -cover ./cmd/senseguard/...` with Go 1.24.13 | PASS, 70.3% statement coverage |
| OPA 1.0.0 `opa test policy -v` | PASS, 16/16 tests |
| `docker compose config --quiet` with a temporary token map | PASS |
| `docker build -t senseguard:verify-20261019 .` | PASS |
| `kubectl kustomize .\k8s` | PASS, local render only |
| Isolated Docker runtime smoke checks | PASS, details below |

The isolated runtime used a new temporary network, the newly built image, random throwaway credentials, a localhost-only published port, and a tmpfs database. It did not mount or modify the existing Compose volume or any database. Verified `/healthz` and `/readyz` (200); missing/invalid credentials (401); caller identity mismatch (403); authenticated budget lookup; invalid mode (400); allowed noisy request (200); denied policy request (403) with no budget charge; exhausted budget (429); and readiness plus sensing failure (503) after stopping the test OPA container. The OPA health-check policy itself evaluated successfully. Only the temporary test containers were stopped afterward.

`kubectl kustomize` parsed and rendered the manifest set, but Kubernetes API schema validation and cluster behavior were not tested. No cluster was contacted. TLS/mTLS handshakes, a real identity provider or token lifecycle, CNI enforcement, storage ownership, image pull from a registry, and real PVC persistence remain unverified.

## Backup and recovery

The Go suite includes a backup/restore test using a separate temporary SQLite database copy and verifies the restored database. This does not validate copying a live or mounted deployment database, backup scheduling, retention, restore objectives, or recovery in a target environment. The README documents an offline, non-overwriting file-copy procedure and restoration to a separate test location. Existing databases and Docker volumes were not used or changed in this verification pass.

## Security and privacy limits

Consumer identity comes from the bearer token map, not a caller-supplied identity header. TLS configuration rejects OPA HTTP downgrade when OPA TLS files or `REQUIRE_TLS` are configured. Noisy demo mode uses `crypto/rand` and fails closed if random generation fails; the distribution has no approved sensitivity, neighboring-data definition, epsilon/delta accountant, or composition model. Coarse output retains types/counts, and small aggregates may reveal individuals. No formal differential-privacy claim is supported.

Static bearer tokens still need approved issuance, secure delivery, rotation, revocation, and least-privilege storage. Compose must remain restricted to the local machine and is not suitable for untrusted callers. Production also requires target-cluster threat review, monitoring and retention controls, measured capacity, tested recovery objectives, and independent security/privacy review. Keep one API replica with this SQLite ledger.

## Secret and change review

The targeted scan of the 12 changed text files found no matches for private-key headers, common GitHub/AWS/Google/Slack token formats, JWT-like strings, or literal bearer credentials. No dedicated secret-scanning tool is installed, and pattern scanning cannot establish that a file contains no sensitive information. The staged patch was manually reviewed and `git diff --cached --check` passed. Ignored archives, backups, database files, and audit copies are excluded from the commit and Docker build context. `.gitignore` excludes environment files, tokens, certificates/keys, database files, backup directories, and ZIP archives. `git diff --check` passed for the current worktree.

The release Go command intentionally targets `./cmd/senseguard/...`. The separate `go test ./...` run also discovered and passed one ignored audit-copy package. Three ignored snapshots contain older Go source, but they are not release files and were not modified. Keeping release CI scoped to the maintained package avoids accidentally treating historical snapshots as maintained application code.

## Repository release status

The current branch is `master`; the configured `origin` is the requested GitHub repository. Publishing the verified changes requires a normal fast-forward push and must preserve the existing branch history and repository-owner author identity. Do not force-push or include ignored local artifacts.

## Files changed in this pass

`.dockerignore`, `README.md`, `VERIFICATION_REPORT.md`, `SECURITY_PRIVACY_REVIEW.md`, `DEPLOYMENT_CHECKLIST.md`, `cmd/senseguard/main.go`, `cmd/senseguard/main_test.go`, `docker-compose.yml`, `k8s/opa-config.yaml`, `k8s/opa-deployment.yaml`, `policy/senseguard.rego`, and `policy/senseguard_test.rego`.
