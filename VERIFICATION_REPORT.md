# SenseGuard verification and deployment report

Date: 2026-10-19. Checks below were run against the current working tree based on commit `956a7d8`; follow-up changes are not committed yet.

## Decision

**A. NOT READY FOR STAGING.** Local code, container, policy, workflow, and upstream manifest-schema checks pass. Target-issued certificates and consumer credentials, target cluster/CNI/storage behavior, an immutable registry image, independent backup/restore operations, production monitoring, and privacy review have not been verified. Use simulated data only. No deployment was performed.

## Architecture and controls

SenseGuard is a Go HTTP API that authenticates consumers using startup-configured bearer tokens, asks OPA to authorize purpose and mode, transforms simulated sensing data, and charges a per-consumer SQLite ledger before returning data. `/budget` uses the authenticated identity; `X-Consumer-ID` cannot select another account. OPA and database failures fail closed. SQLite supports one API replica.

Compose is a localhost-only development profile. API-to-OPA traffic is HTTP on its private Compose network; the OPA API does not authenticate Compose clients. Both containers are non-root and restricted. The meaningful OPA health check tests a policy decision and its loopback diagnostic endpoint. OPA telemetry is disabled.

The root Kustomize set generates its OPA ConfigMap from the same Rego files under `policy/`, with a content hash that triggers rollout when policy changes. Kubernetes configures HTTPS for the API and mTLS plus OPA API authorization from SenseGuard to OPA. NetworkPolicies restrict OPA ingress to SenseGuard and API ingress to explicitly labeled client pods in approved namespaces. Network enforcement depends on the target CNI. The API Service is internal ClusterIP; no Ingress is configured. Kubernetes still uses `senseguard:local`, which must be replaced with a reviewed registry image pinned by digest before rollout.

The database backup command uses `VACUUM INTO` to create a consistent online snapshot at a new, non-overwriting path, then checks SQLite integrity and the expected ledger table. The destination directory must exist and be writable. The command does not schedule backups, export them off-volume, or define retention/recovery objectives.

## Checks executed on the current working tree

| Check | Result |
|---|---|
| `go mod verify` with Go 1.26.9 | PASS, all modules verified |
| `go test ./cmd/senseguard/...` with Go 1.26.9 | PASS |
| `go vet ./cmd/senseguard/...` with Go 1.26.9 | PASS, no diagnostics |
| `go test -race ./cmd/senseguard/...` with Go 1.26.9 | PASS |
| `go test -cover ./cmd/senseguard/...` with Go 1.26.9 | PASS, 70.5% statement coverage |
| `govulncheck` v1.1.4 with Go 1.26.9 | PASS, no known vulnerabilities reported in reachable code or required modules |
| OPA 1.0.0 `opa test /policy -v` | PASS, 16/16 |
| OPA 1.0.0 `run --help` telemetry flag check | PASS, `--disable-telemetry` supported |
| `docker compose config --quiet` with a non-production temporary token map | PASS |
| `docker build -t senseguard:verify-20261019-go1269 .` | PASS; resulting image is present locally |
| `kubectl kustomize .` | PASS; root Kustomize render uses source policies |
| Strict Kubeconform validation | PASS, 9 valid resources, 0 invalid |
| `actionlint` on `.github/workflows/verify.yml` | PASS |
| Isolated Docker runtime smoke test | PASS; results below |
| SQLite online backup and separate restore unit test | PASS |
| Backup CLI rejects an invalid source without changing its bytes | PASS |
| Go mTLS integration test using an ephemeral local test CA | PASS; OPA server identity is verified and a client certificate is presented |

Go 1.24.13's standard library scan identified known vulnerabilities, including a remotely triggerable HTTP/2 server crash. The project was moved to Go 1.26.9 and `golang.org/x/sys` was upgraded to v0.44.0; the final Go 1.26.9 `govulncheck` run reported no known vulnerabilities. A scan with Go 1.27.2 could not be completed because the pinned scanner's parser panicked on that newer Go syntax, so the checked and pinned toolchain is Go 1.26.9. The initial Alpine race-detector attempt lacked cgo; the final Go 1.26.9 race run passed. Go checks ran in disposable containers against the current project source; no persistent database volume was mounted.

Kubeconform validated the rendered resources against upstream schemas. This did not contact an API server and is not validation against a selected target Kubernetes version. The first pushed GitHub Actions run succeeded for commit `8e65fe5`. GitHub noted Node 20 action-runtime and `ubuntu-latest` migration warnings; the workflow now pins official Node 24 action releases by full SHA and fixes the runner at Ubuntu 24.04. The updated workflow run must pass before treating this check as complete. The workflow is verification-only with `contents: read`. The Docker image build uses the patched Go 1.26.9 builder. Local Compose remains HTTP-only, loopback-bound, and intended for development.

The isolated runtime used a unique temporary network, random throwaway credentials, and a tmpfs database. `/readyz` returned 200; missing credentials returned 401; identity mismatch and policy denial returned 403; the denied request did not charge budget; allowed requests returned 200; an exhausted budget returned 429; the online backup and integrity check succeeded; and after OPA stopped, readiness and sensing returned 503. Temporary containers and the network were removed. No existing Docker volume or database was mounted or changed.

The Go test creates a snapshot while the ledger is open, verifies integrity, refuses an existing destination without modifying it, restores the snapshot to another temporary file, and checks that the known balance is preserved.

## Security and privacy limits

Consumer identity comes from a bearer-token map; tokens still need secure issuance, delivery, revocation, rotation, and Secret management. No identity provider is integrated. A Go integration test with ephemeral test-only certificates verifies that SenseGuard trusts its configured OPA CA and presents a client certificate to an mTLS OPA endpoint. This does not validate real target certificates, trust chains, renewal, DNS names, or target TLS policy. Compose is not safe for untrusted network callers.

No formal differential-privacy guarantee is established. The ledger is a per-request counter rather than epsilon/delta accounting; noise lacks an approved sensitivity, neighboring-data model, and composition accountant. Coarse output retains object types/counts, and small aggregates can expose individuals. The app currently returns simulated data only.

Production operations remain unverified: target PVC and `fsGroup` behavior, CNI NetworkPolicy enforcement, independent backup retention and recovery objectives, monitoring/log retention, rate limiting, measured capacity, rollback, and independent security/privacy review. Keep one API replica.

## Secret, artifact, and change review

The existing ignored backups, archives, local databases, and audit copies were left untouched. The project `.gitignore` excludes environment files, tokens, certificates/keys, local databases, backup directories, and ZIP archives. A targeted scan of changed and untracked release files found no private-key blocks, common GitHub/AWS token formats, JWT-like strings, or credential assignments. `gitleaks` is not installed, so this pattern scan cannot establish that all sensitive information is absent. `git diff --check` passed. Ignored local artifacts are outside the release file list.

## Repository release status

The current branch is `master` and `origin` is configured for the requested SenseGuard GitHub repository. Immediately before release preparation, `origin/master` matched local commit `956a7d8af3ae7041514b7daab9f30015cf2a8d8c`, allowing a normal fast-forward. The exact commit and push outcome are recorded in the release summary. GitHub-hosted CI must still be checked for the published commit.

## Follow-up files changed

`.github/workflows/verify.yml`, `README.md`, `DEPLOYMENT_CHECKLIST.md`, `SECURITY_PRIVACY_REVIEW.md`, `VERIFICATION_REPORT.md`, `cmd/senseguard/ledger_sqlite.go`, `cmd/senseguard/main.go`, `cmd/senseguard/main_test.go`, `docker-compose.yml`, `k8s/opa-deployment.yaml`, `k8s/senseguard-deployment.yaml`, `k8s/senseguard-networkpolicy.yaml`, and root `kustomization.yaml`. The duplicate embedded policy file `k8s/opa-config.yaml` and nested Kustomize entrypoint `k8s/kustomization.yaml` were removed because the root Kustomize generator now consumes the canonical `policy/` files.
