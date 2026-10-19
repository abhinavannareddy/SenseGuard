# Security and privacy review

Date: 2026-10-19. This is a source/configuration review, not a penetration test. Use simulated data only.

## Implemented controls

- `/sensing` and `/budget` require bearer credentials. Identity is derived from the matching secret token, not `X-Consumer-ID`. Mismatched caller claims are rejected. Tokens must be unique and at least 32 characters, are hashed in memory for lookup, and are not logged. Production credentials are supplied from an external secret configuration; none are committed.
- `REQUIRE_TLS` rejects malformed values instead of treating typos as disabled TLS. Duplicate Authorization headers are rejected. The OPA TLS client trusts only the CA configured for OPA.
- Kubernetes requires API HTTPS and HTTPS with mutual TLS to OPA. The API verifies OPA's server certificate against a configured CA. OPA requires a client certificate, and its `system.authz` policy pins the certificate subject to `CN=senseguard-api`; it grants only the policy decision and `/health` endpoints. SenseGuard explicitly enforces TLS 1.2 or newer; OPA uses its configured TLS listener defaults, which must be checked against transport-security requirements for the target environment.
- Kubernetes OPA ingress is restricted to SenseGuard pods by a NetworkPolicy. This depends on a CNI that enforces NetworkPolicy. The app Service is ClusterIP-only; no Ingress is defined.
- Compose ports bind to localhost. Its OPA API is HTTP inside the Compose network for local development. Its health check evaluates a known allowed policy decision and calls OPA's loopback-only diagnostic `/health` endpoint; SenseGuard readiness checks SQLite and OPA health.
- OPA authorization runs before budget charging. OPA transport failures, timeouts, non-200 responses, malformed JSON, and missing decisions fail closed.
- SQLite statements are parameterized. Budget updates are transactional, validate finite positive costs, and return data only after a confirmed commit.
- The SenseGuard image runs as non-root. Compose uses a read-only root filesystem, drops all capabilities, applies CPU/memory limits, and enables no-new-privileges. Kubernetes containers run non-root, disable service-account token mounting, use read-only roots, drop capabilities, apply seccomp and resource limits. The SQLite ledger is designed for a single SenseGuard replica.
- Coarse mode rounds coordinates and redacts IDs. Aggregate mode returns counts. Noisy mode adds pseudo-random Laplace-shaped noise.

Go authentication/TLS tests and the API runtime smoke check passed in the preceding engineering pass. The newly added OPA `system.authz` tests and updated OPA startup configuration have not run because OPA is unavailable in this session. Docker Compose config and local Kustomize rendering have passed; the Docker Engine is unavailable for a current image build or runtime test. See `VERIFICATION_REPORT.md` for the exact results and limits.

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

1. Run Go tests, race tests, vet, OPA tests, Compose validation, and Docker build against the changed code.
2. Render the manifests with `kubectl kustomize .\k8s`, validate their Kubernetes schemas offline, and verify the target CNI enforces NetworkPolicy.
3. Provide real trusted API, OPA server, OPA client, and CA certificates using external secret management; verify API and OPA DNS names, OPA client subject `CN=senseguard-api`, trust chains, expiry, and rotation.
4. Provision consumer bearer tokens through an identity owner, configure Secret encryption/RBAC, and test authentication, impersonation, revocation, and rotation.
5. Test SQLite backup/restore to a separate database and document recovery objectives.
6. Perform a restricted runtime staging validation, including readiness, authorization, OPA failure, budget exhaustion, and TLS verification.

## Required before production

Define a reviewed privacy model/accountant before making formal privacy claims; complete threat modeling, rate limiting and abuse controls at the approved gateway, monitoring and retention/log safeguards, measured capacity planning, target-cluster storage validation, and independent security/privacy review. Establish a supported shared budget design before scaling beyond one app replica.

The OPA authentication and authorization configuration follows the [OPA security guidance](https://www.openpolicyagent.org/docs/security), which distinguishes client TLS authentication from API authorization and documents the request identity and path fields used by `system.authz`.

## Verdict

**NOT YET READY FOR STAGING ROLLOUT OR PRODUCTION.** Identity and TLS controls are now implemented in code/configuration, but the changes and target deployment environment still need verification and operational secrets/certificates.
