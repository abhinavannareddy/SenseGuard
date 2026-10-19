# SenseGuard verification and deployment report

Date: 2026-10-19

## Decision

**NOT READY FOR STAGING OR PRODUCTION.** The implementation and local configuration have been reviewed, but the current Go/OPA suite, container runtime, Kubernetes schema validation, and staging checks could not be completed in this environment. Do not deploy until those checks pass.

## Current architecture and controls

SenseGuard is a Go HTTP service using OPA for allow/deny decisions and SQLite for transactional per-consumer request counters. It returns simulated data only. `/sensing` and `/budget` require bearer credentials; the authenticated identity is mapped from a startup secret and used for OPA and SQLite. A conflicting `X-Consumer-ID` claim is rejected. Credentials are not logged and are removed from the app's retained config after hashing for lookup.

`REQUIRE_TLS=true` requires the API certificate/key, an HTTPS `OPA_URL`, a CA for verifying OPA, and a client certificate/key. The Kubernetes manifest enables API HTTPS and mTLS to OPA. OPA requires TLS client authentication. An OPA ingress NetworkPolicy permits SenseGuard pods only, subject to CNI enforcement. The API Service is ClusterIP-only; no Ingress or external exposure is configured.

Compose remains a localhost-only HTTP development setup, requires `CONSUMER_TOKENS`, and waits for an OPA health check that evaluates a known policy decision and calls OPA's `/health` endpoint. SenseGuard readiness checks both SQLite and OPA.

The SQLite ledger uses parameterized statements and transactions, enforces a 10,000-consumer cap, rejects invalid charges, and returns data only after a confirmed commit. Kubernetes is configured for one app replica. PVC persistence is not a backup.

## Earlier verification results (prior revision)

The following checks were recorded before the authentication/TLS update. They are historical and do not verify the current changes:

- `go test ./...`, `go vet ./...`, `go test -race ./cmd/senseguard/...` passed.
- Main package statement coverage was 72.3%.
- OPA policy tests passed 11/11.
- Compose validation, Docker image build, local startup, `/healthz`, `/readyz`, authorization/budget behavior, and persistence across app-container recreation were recorded as passing.
- The earlier runtime persistence check used the named volume. This pass did not inspect or modify the database or Docker volumes.

## Current pass checks

- Source and configuration review completed. Tests cover authentication failures and impersonation, duplicate Authorization headers, strict TLS configuration, OPA URL validation, and SQLite backup-copy restoration. Go commands now target only `cmd/senseguard`, avoiding stale ignored audit snapshots.
- `git diff --check` passed after removing an extra blank line at the end of `policy/senseguard_test.rego`.
- `docker compose config --quiet` passed with a validation-only token in the environment. Docker emitted a warning that its user configuration was inaccessible; no secret value was printed.
- Go tests, vet, race tests, and coverage could not run because Go is not installed or on PATH. An attempted checksum-verified download into a temporary folder did not produce a usable archive. OPA tests could not run because OPA is not installed or on PATH.
- Docker image build and runtime checks could not run: the Docker Engine named pipe is unavailable. Docker containers and volumes were not changed. The preapproved runtime smoke check could not connect to `127.0.0.1:19467`.
- Kubernetes client parsing could not complete because kubectl attempted API discovery and no cluster is reachable. No manifests were applied.
- Backup/restore against a deployed volume, target-cluster TLS/certificate validation, CNI enforcement, and storage behavior remain unverified.
- No production, cloud, or Kubernetes deployment was performed. No repository remote is configured, so there was no push.

## Privacy and remaining deployment blockers

The request budget is a per-request counter, not formal epsilon accounting. Noisy mode uses pseudo-random Laplace-shaped noise without defined sensitivity, neighboring-data model, or accountant. Coarse output retains types/counts; small aggregate groups may reveal individuals. No formal differential privacy claim is supported.

Before restricted staging, run the current test/build suite, validate manifests offline, supply certificates and credentials from an approved secret manager, verify Kubernetes Secret encryption/RBAC and NetworkPolicy enforcement, confirm storage/permissions/image availability, and run a restricted staging smoke test including TLS, authentication, impersonation, OPA failure, budget exhaustion, readiness, and recovery.

Before production, additionally complete tested backup/recovery objectives, formal privacy review/accounting, monitoring/retention controls, capacity planning, target-cluster validation, and independent security/privacy review. Do not scale the SQLite deployment beyond one replica.

No production, cloud, or Kubernetes deployment has been performed. GitHub push was not performed; this repository has no configured remote.

## Changed files in this revision

`.gitignore`, `README.md`, `DEPLOYMENT_CHECKLIST.md`, `SECURITY_PRIVACY_REVIEW.md`, `VERIFICATION_REPORT.md`, `cmd/senseguard/main.go`, `cmd/senseguard/main_test.go`, `docker-compose.yml`, `k8s/opa-config.yaml`, `k8s/opa-deployment.yaml`, `k8s/senseguard-deployment.yaml`, `policy/senseguard.rego`, and `policy/senseguard_test.rego`.
