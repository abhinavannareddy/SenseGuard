# Security and privacy review

Date: 2026-10-19. This is a source/configuration review, not a penetration test. Use simulated data only.

## Implemented controls

- `/sensing` and `/budget` require bearer credentials. Identity is derived from the matching secret token, not `X-Consumer-ID`; mismatched claims are rejected. Tokens must be unique and at least 32 characters, are hashed in memory, and are not logged. Production token issuance, storage, rotation, and revocation remain operator responsibilities.
- `REQUIRE_TLS` rejects malformed values and requires API TLS plus OPA CA and client certificates. Any OPA TLS material forces an HTTPS OPA URL. The OPA client trusts only the configured CA.
- Kubernetes configures API HTTPS and mTLS from SenseGuard to OPA. OPA pins the client subject to `CN=senseguard-api` and authorizes only the policy decision and health endpoint. The app enforces TLS 1.2 or newer. The target PKI, certificate rotation, DNS names, and target TLS policy have not been verified.
- Kubernetes NetworkPolicies restrict OPA ingress to SenseGuard and API ingress to client pods explicitly labeled in approved namespaces. NetworkPolicy enforcement depends on the target CNI, and policies are additive if other policies also select these pods. The app Service is ClusterIP-only; no Ingress is defined.
- The root Kustomize set generates the OPA ConfigMap from the same tested files under `policy/`, and the content hash triggers a rollout when policies change. Kubernetes startup probes allow initialization before liveness/readiness checks take over.
- OPA telemetry is disabled in Compose and Kubernetes; the pinned OPA 1.0.0 CLI advertises the configured flag.
- Compose ports bind to localhost and its API-to-OPA connection uses unauthenticated HTTP on the private Compose network. Compose is local development only.
- OPA authorization runs before budget charging. OPA failures, timeouts, non-200 responses, malformed JSON, and missing decisions fail closed. The service returns data only after a confirmed budget commit.
- SQLite statements are parameterized; budget updates are transactional; invalid cost values are rejected. Database files and generated snapshots use restrictive POSIX permissions where available.
- The `backup` command uses SQLite `VACUUM INTO`, refuses to overwrite an existing path, and verifies snapshot integrity and ledger schema. A separate restore test checks the expected balance. This does not configure automated or off-site backups.
- Containers run non-root with read-only root filesystems, dropped capabilities, no-new-privileges, seccomp, and resource limits. SQLite is a single-replica ledger.
- Coarse mode rounds coordinates and redacts IDs; aggregate mode returns counts. Noisy mode uses cryptographic randomness for demo noise but does not provide formal differential privacy.
- A GitHub Actions verification workflow runs tests, OPA checks, image build, Compose validation, and Kubernetes schema validation with `contents: read`; it does not deploy or publish images. Both the first hosted run and the updated workflow run passed. The actions are pinned by full SHA to Node 24-compatible releases, and the runner is pinned to Ubuntu 24.04.

## Verification on the current working tree

Go 1.26.9 application tests, `go vet`, and race tests passed; statement coverage is 70.5%. `go mod verify` passed, and `govulncheck` v1.1.4 reported no known vulnerabilities for the final Go 1.26.9 toolchain and module set. Go 1.24.13 scanning had identified standard-library vulnerabilities and `golang.org/x/sys` v0.34.0 was affected by a module advisory; the toolchain and `x/sys` were upgraded. OPA 1.0.0 tests passed 16/16. A Go mTLS integration test with ephemeral certificates passed and verifies the OPA server CA and client certificate exchange; it does not validate deployment PKI. Compose config validation, Docker build, Kustomize render, strict Kubeconform validation (9 valid resources), and actionlint passed. An isolated runtime test verified 401 unauthenticated access, 403 identity mismatch and policy denial, no charge on denial, allowed requests, 429 budget exhaustion, an online SQLite snapshot, and 503 fail-closed behavior during OPA outage. The Go backup tests verify non-overwrite, restoration of the expected balance, and rejection of an invalid source without changing its bytes. No existing database or named volume was mounted or changed. GitHub-hosted CI and deployment behavior remain unverified; details are in `VERIFICATION_REPORT.md`.

## Remaining risks and deployment limits

- Bearer credentials are static until rotated. No identity provider or token refresh is integrated. Configure Kubernetes Secret encryption at rest and least-privilege RBAC; establish issuance, revocation, and rotation procedures.
- The Service is internal and there is no external Ingress, DNS, firewall, or gateway configuration. Any external access requires a reviewed gateway with TLS handling, authentication, and rate limiting.
- No target-issued TLS certificates or actual target-cluster TLS/mTLS handshake has been verified. The passing local handshake test uses ephemeral test certificates only. Protect the CA and restrict issuance of the pinned OPA client identity to SenseGuard.
- NetworkPolicy CNI enforcement and approved client namespace/pod labels need target verification.
- No formal differential privacy. The budget is a per-request counter, not epsilon accounting. Noise has no approved sensitivity, neighboring-data model, or accountant. Coarse responses retain types/counts, and small aggregates can expose individuals.
- Backups are not scheduled or exported to independent storage by the application. RPO/RTO, retention, access control, and a target storage recovery drill remain unverified. SQLite is not application-encrypted.
- The Kubernetes schema check uses upstream schemas but is not tied to a target Kubernetes version and is not an API-server dry run. StorageClass, volume ownership, `fsGroup`, image availability, persistence, and CNI behavior require target validation. Keep one app replica.
- The deployment manifest still uses `senseguard:local`; a reviewed registry image and immutable digest are needed for a cluster rollout.
- Logs can include authenticated consumer IDs and purposes. Resource values have not been load-tested; monitoring, retention/redaction, capacity planning, and incident response are incomplete.
- The application serves simulated sensing data only. A reviewed data source, legal basis, privacy model, and retention policy are required before any real sensing data is processed.

## Required before restricted staging

1. Confirm the GitHub Actions workflow passes for the exact release commit.
2. Validate the rendered manifests against the target Kubernetes version and admission policies.
3. Issue and test the API/OPA certificates through approved PKI; verify DNS SANs, trust chains, expiry, rotation, and the OPA client subject.
4. Provision consumer tokens through an identity owner and protect them with approved secret management, RBAC, revocation, and rotation.
5. Verify the target CNI, approved client labels, storage class, volume permissions, and persistence.
6. Export an online SQLite snapshot to independent protected storage and complete a restore drill with recovery objectives.
7. Complete monitoring, logging/retention, gateway abuse controls, rollback, and restricted staging runtime validation.

## Required before production

Complete threat modeling and independent security/privacy review, capacity/load validation, incident and recovery procedures, and production PKI/identity operations. For real sensing data, approve a privacy model and legal/retention controls; build and validate a formal accountant before making any differential-privacy claim. Establish a supported shared budget design before scaling beyond one API replica.

The OPA authentication and authorization configuration follows the [OPA security guidance](https://www.openpolicyagent.org/docs/security), which distinguishes client TLS authentication from API authorization and documents the request identity and path fields used by `system.authz`.

## Verdict

**NOT YET READY FOR STAGING ROLLOUT OR PRODUCTION.** Repository controls and local checks improved, but target credentials, PKI, cluster policy/storage behavior, image publication, independent recovery, production operations, and privacy review remain outstanding.
