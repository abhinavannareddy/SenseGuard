# SenseGuard verification and deployment report

Date: 2026-10-19

## Decision

**A. NOT READY FOR STAGING.** The current OPA client-identity authorization policy and deployment configuration still need their full test/build suite and target-environment validation. Use simulated data only. No deployment was performed.

## Architecture and security controls

SenseGuard is a Go HTTP API that authenticates consumers with startup-configured bearer tokens, asks OPA to authorize purpose and privacy mode, transforms simulated sensing data, and charges a per-consumer SQLite ledger before returning the response. The budget endpoint uses the same authenticated identity as sensing; a caller-supplied `X-Consumer-ID` cannot select another account. OPA errors and database errors fail closed.

Compose is a localhost-only development profile using HTTP. Its OPA health check evaluates a known policy decision and queries a loopback-only diagnostic endpoint. SenseGuard readiness checks both SQLite and OPA. The Compose services now drop Linux capabilities, use read-only root filesystems and no-new-privileges, and have CPU and memory limits.

The Kubernetes manifests are preparation only. They require API HTTPS and mTLS to OPA, pin the expected OPA client subject to `CN=senseguard-api`, restrict OPA's API to the decision and health paths, disable service-account token mounting, apply non-root/read-only/capability/seccomp controls, and keep the Service internal. NetworkPolicy behavior depends on the cluster CNI. The SQLite PVC is a single-replica design and is not a backup.

The README includes request-flow and deployment-topology diagrams plus compressed illustrative images for the privacy modes. The visuals describe demo behavior and make no privacy-guarantee claim.

## Checks and results

### Ran against this documentation/configuration pass

- **PASS** `docker compose config --quiet` using an ephemeral validation-only token map. No token value was printed or saved.
- **PASS** `kubectl kustomize .\k8s` rendered the full resource set locally with kubectl client 1.36.1 and Kustomize 5.8.1. This checks Kustomize/YAML parsing; it does not validate Kubernetes schemas or contact a cluster.
- **PASS** `git diff --check` on the tracked diff and a separate trailing-whitespace check on new Rego/YAML files.
- **PASS** A targeted scan of 29 source/document text files found no private-key headers, common GitHub/AWS token formats, literal bearer credentials, or cleartext password/API-key assignments. No dedicated secret-scanning tool is installed; binary illustrations and ignored local archives/backups are not part of the candidate changes. `.gitignore` rules for environment files, consumer tokens, certificate/key files, databases, backup folders, and ZIP archives were checked.
- **PASS** The configured Git author and current commit author are the repository owner; no contributor or repository settings were changed.

### Earlier checks on the current Go implementation

The Go authentication/TLS source was unchanged in this visual/documentation pass. Previous execution results were:

- `go test ./cmd/senseguard/...` passed, including the stopped-database-copy restore test (`ok senseguard/cmd/senseguard 2.245s`).
- `go vet ./cmd/senseguard/...` and `go test -race ./cmd/senseguard/...` passed before that final test-only backup-copy addition. Coverage was 67.4% before that addition. An earlier pre-authentication revision had 72.3% coverage; that number does not describe the current implementation.
- OPA policy tests passed 11/11 before four tests were added for OPA API authorization. The current 15-test suite has **not** run.
- A fresh app runtime check passed earlier for unauthenticated/invalid credentials (401), identity spoofing (403), authorized requests (200), denied requests (403 without budget charge), health/readiness (200), and budget exhaustion (429). This did not include the newly added OPA `system.authz` identity/path restriction.
- An OPA 1.0.0 runtime check had verified the earlier policy health rule. The new OPA client authorization rules have not been run.
- `docker compose config --quiet` and `kubectl kustomize .\k8s` passed in this pass. The Docker image build and runtime were not repeated because the Docker Engine named pipe is unavailable. No containers or volumes were changed.

Go and OPA are not installed on PATH in this session. Docker CLI is installed, but the Docker Desktop Linux engine is unavailable. No Kubernetes cluster is connected. `kubectl kustomize` was available and performed the local render described above. Kubernetes schema validation, cluster behavior, certificate trust, CNI enforcement, storage permissions, and PVC persistence remain unverified.

## Backup and recovery

The source tests cover copying a closed SQLite database to a separate file and reopening it. No backup or restore was performed against the existing Compose volume or any real deployment. The README now documents a timestamped, non-overwriting offline copy procedure that restarts the service even if copying fails, and requires restoring only to a separate test directory. Recovery objectives and a real restore exercise remain outstanding. Existing databases and Docker volumes were not modified in this pass.

## Privacy and deployment blockers

Budget units are per-request counters, not epsilon accounting. Noisy mode uses pseudo-random Laplace-shaped noise without a defined sensitivity, neighboring-data model, or accountant. Coarse output retains types/counts, and small aggregates may reveal individuals. No formal differential-privacy claim is supported.

Before restricted staging, run the current Go and 15-test OPA suites, vet, race, coverage, Compose validation, and Docker build/runtime checks; validate Kubernetes schemas offline; provision real certificates and consumer credentials through approved secret management; verify Secret encryption/RBAC, TLS subject/SANs, CNI enforcement, storage permissions, image availability, and persistence; and test recovery on a separate database.

Before production, also complete formal privacy accounting and review, backup/recovery objectives, monitoring and retention controls, capacity planning, target-cluster validation, and independent security/privacy review. Keep one SenseGuard replica unless the budget ledger is redesigned for shared transactional storage.

## Repository state

The current branch is `master`, with the configured GitHub remote `origin` pointing at the requested SenseGuard repository. No collaborators or repository settings were changed. Commits use the locally configured repository-owner identity; use repository history to determine the published commit.

## Files changed in this pass

`.gitignore`, `README.md`, `DEPLOYMENT_CHECKLIST.md`, `SECURITY_PRIVACY_REVIEW.md`, `VERIFICATION_REPORT.md`, `docker-compose.yml`, `k8s/kustomization.yaml`, `k8s/opa-config.yaml`, `k8s/opa-deployment.yaml`, `k8s/senseguard-deployment.yaml`, `policy/opa_authz.rego`, `policy/senseguard_test.rego`, `docs/assets/senseguard-hero.jpg`, and `docs/assets/privacy-modes.jpg`.
