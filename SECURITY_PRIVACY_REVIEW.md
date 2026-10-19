# Security and privacy review

Date: 2026-10-19. This is a source/configuration review, not a penetration test. Use simulated data only.

## Implemented controls

- `/sensing` and `/budget` require bearer credentials. Identity is derived from the matching secret token, not `X-Consumer-ID`. Mismatched caller claims are rejected. Tokens must be unique and at least 32 characters, are hashed in memory for lookup, and are not logged. Production credentials are supplied from an external secret configuration; none are committed.
- `REQUIRE_TLS` rejects malformed values instead of treating typos as disabled TLS. Any configured OPA TLS material requires an HTTPS OPA URL; required TLS needs the CA and client certificate/key. Duplicate Authorization headers are rejected. The OPA TLS client trusts only the CA configured for OPA.
- Kubernetes requires API HTTPS and HTTPS with mutual TLS to OPA. The API verifies OPA's server certificate against a configured CA. OPA requires a client certificate, and its `system.authz` policy pins the certificate subject to `CN=senseguard-api`; it grants only the policy decision and `/health` endpoints. SenseGuard explicitly enforces TLS 1.2 or newer; OPA uses its configured TLS listener defaults, which must be checked against transport-security requirements for the target environment.
- Kubernetes OPA ingress is restricted to SenseGuard pods by a NetworkPolicy. This depends on a CNI that enforces NetworkPolicy. The app Service is ClusterIP-only; no Ingress is defined.
- Compose ports bind to localhost. Its OPA API uses HTTP inside the Compose network and does not authenticate API clients, so Compose is local development only. Its health check evaluates a known allowed policy decision, confirms the authorization policy is loaded, and calls OPA's loopback-only diagnostic `/health` endpoint; SenseGuard readiness checks SQLite and OPA health.
- OPA authorization runs before budget charging. OPA transport failures, timeouts, non-200 responses, malformed JSON, and missing decisions fail closed.
- SQLite statements are parameterized. Budget updates are transactional, validate finite positive costs, and return data only after a confirmed commit.
- The SenseGuard image runs as non-root. Compose uses a read-only root filesystem, drops all capabilities, applies CPU/memory limits, and enables no-new-privileges. Kubernetes containers run non-root, disable service-account token mounting, use read-only roots, drop capabilities, apply seccomp and resource limits. The SQLite ledger is designed for a single SenseGuard replica.
- Coarse mode rounds coordinates and redacts IDs. Aggregate mode returns counts. Noisy mode samples Laplace-shaped demonstration noise with Go's cryptographically secure random source and fails closed if random generation fails; this does not provide formal differential privacy.

Current-source verification passed on 2026-10-19 using Go 1.24.13 and OPA 1.0.0 containers: Go tests, vet, race tests, 70.3% coverage, and all 16 OPA tests. Compose configuration, Docker image build, local Kustomize rendering, and an isolated runtime smoke test also passed. The runtime test confirmed authentication failures, caller identity mismatch rejection, denied requests not charging budget, budget exhaustion, readiness, and fail-closed behavior when OPA stops. It used temporary credentials and a tmpfs database; no existing database or volume was touched. See `VERIFICATION_REPORT.md` for commands and detailed scope.

## Remaining risks and deployment limits

- Bearer credentials are static until rotated. Their secure issuance, distribution, revocation, and storage are operational responsibilities. Kubernetes Secrets need encryption at rest and least-privilege RBAC. No identity provider or token refresh is integrated.
- The Kubernetes app Service is ClusterIP-only. There is no external Ingress, DNS, certificate issuance, or firewall configuration. Any external access requires a separately reviewed TLS-preserving/reencrypting gateway and network policy.
- Kubernetes OPA TLS authentication validates certificates issued by the configured CA, and its authorization policy pins the client subject. Restrict issuance of `CN=senseguard-api` to SenseGuard and protect CA keys. NetworkPolicy enforcement depends on the target CNI.
- Compose remains HTTP development only and must stay on a trusted local machine. Do not use it for untrusted callers.
- No formal differential privacy. The budget is a per-request counter, not epsilon accounting. Noise has no defined sensitivity, neighboring-data model, or accountant. Coarse output retains types/counts, and small aggregates can expose individuals.
- Backup/recovery is not validated. The persistent volume is not a backup. SQLite is not application-encrypted.
- Kubernetes schema validation and target-cluster behavior are not verified. StorageClass, volume permissions, `fsGroup`, Secret encryption, CNI policy enforcement, image availability, and persistence require target validation. Keep one app replica.
- Logs can include authenticated consumer IDs and purposes. Resource values have not been load-tested.

## Required before any staging rollout

1. Re-run the passing Go tests, race tests, vet, OPA tests, Compose validation, and Docker build in release CI against the exact release commit.
2. Validate rendered manifests against the target Kubernetes API schema; verify the target CNI enforces NetworkPolicy and confirm the cluster's supported TLS policy.
3. Provide trusted API, OPA server, OPA client, and CA certificates using approved secret management; verify DNS names, OPA client subject `CN=senseguard-api`, trust chains, expiry, and rotation with the target environment.
4. Provision consumer bearer tokens through an identity owner; configure Secret encryption/RBAC and establish issuance, revocation, and rotation procedures.
5. Run an environment-specific SQLite backup and restore drill against a separate database and set recovery objectives.
6. Complete restricted staging validation for certificates, authentication, authorization, OPA outage, budget exhaustion, storage persistence, monitoring, and rollback before any staging rollout.

## Required before production

Define a reviewed privacy model/accountant before making formal privacy claims; complete threat modeling, rate limiting and abuse controls at the approved gateway, monitoring and retention/log safeguards, measured capacity planning, target-cluster storage validation, and independent security/privacy review. Establish a supported shared budget design before scaling beyond one app replica.

The OPA authentication and authorization configuration follows the [OPA security guidance](https://www.openpolicyagent.org/docs/security), which distinguishes client TLS authentication from API authorization and documents the request identity and path fields used by `system.authz`.

## Verdict

**NOT YET READY FOR STAGING ROLLOUT OR PRODUCTION.** Local code, container, and simulated runtime checks pass. Real credentials and certificates, Kubernetes schema and target-cluster verification, storage and recovery validation, and the staging operational plan remain outstanding.
