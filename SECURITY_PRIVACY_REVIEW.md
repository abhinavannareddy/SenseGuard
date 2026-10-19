# Security and privacy review

Date: 2026-10-19. This is a source/configuration review, not a penetration test. Use simulated data only.

## Implemented controls

- `/sensing` and `/budget` require bearer credentials. Identity is derived from the matching secret token, not `X-Consumer-ID`. Mismatched caller claims are rejected. Tokens must be unique and at least 32 characters, are hashed in memory for lookup, and are not logged. Production credentials are supplied from an external secret configuration; none are committed.
- `REQUIRE_TLS` rejects malformed values instead of treating typos as disabled TLS. Duplicate Authorization headers are rejected. The OPA TLS client trusts only the CA configured for OPA.
- Kubernetes requires API HTTPS and HTTPS with mutual TLS to OPA. The API verifies OPA's server certificate against a configured CA. OPA requires a client certificate. TLS 1.2 or newer is configured for both endpoints.
- Kubernetes OPA ingress is restricted to SenseGuard pods by a NetworkPolicy. This depends on a CNI that enforces NetworkPolicy. The app Service is ClusterIP-only; no Ingress is defined.
- Compose ports bind to localhost. Its OPA health check evaluates a known allowed policy decision and calls the OPA `/health` endpoint; SenseGuard readiness checks SQLite and OPA health.
- OPA authorization runs before budget charging. OPA transport failures, timeouts, non-200 responses, malformed JSON, and missing decisions fail closed.
- SQLite statements are parameterized. Budget updates are transactional, validate finite positive costs, and return data only after a confirmed commit.
- Containers use non-root identities, read-only root filesystems, dropped capabilities, seccomp, resource requests/limits, and no-new-privileges in Compose. The SQLite ledger is designed for a single SenseGuard replica.
- Coarse mode rounds coordinates and redacts IDs. Aggregate mode returns counts. Noisy mode adds pseudo-random Laplace-shaped noise.

Historical test and runtime results refer to an earlier revision. The authentication, TLS, and manifest changes in the current work have not yet passed Go or OPA tests because those tools are unavailable; Docker Engine access is unavailable in this session.

## Remaining risks and deployment limits

- Bearer credentials are static until rotated. Their secure issuance, distribution, revocation, and storage are operational responsibilities. Kubernetes Secrets need encryption at rest and least-privilege RBAC. No identity provider or token refresh is integrated.
- The Kubernetes app Service is ClusterIP-only. There is no external Ingress, DNS, certificate issuance, or firewall configuration. Any external access requires a separately reviewed TLS-preserving/reencrypting gateway and network policy.
- OPA's TLS client authentication trusts any client certificate issued by the configured CA. Restrict issuance of that client certificate to SenseGuard and protect CA keys. NetworkPolicy enforcement depends on the target CNI.
- Compose remains HTTP development only and must stay on a trusted local machine. Do not use it for untrusted callers.
- No formal differential privacy. The budget is a per-request counter, not epsilon accounting. Noise has no defined sensitivity, neighboring-data model, or accountant. Coarse output retains types/counts, and small aggregates can expose individuals.
- Backup/recovery is not validated. The persistent volume is not a backup. SQLite is not application-encrypted.
- Kubernetes schema validation and target-cluster behavior are not verified. StorageClass, volume permissions, `fsGroup`, Secret encryption, CNI policy enforcement, image availability, and persistence require target validation. Keep one app replica.
- Logs can include authenticated consumer IDs and purposes. Resource values have not been load-tested.

## Required before any staging rollout

1. Run Go tests, race tests, vet, OPA tests, Compose validation, and Docker build against the changed code.
2. Validate Kubernetes manifests with an offline schema validator and verify the target CNI enforces NetworkPolicy.
3. Provide real trusted API, OPA server, OPA client, and CA certificates using external secret management; verify their names, trust chain, expiry, and rotation.
4. Provision consumer bearer tokens through an identity owner, configure Secret encryption/RBAC, and test authentication, impersonation, revocation, and rotation.
5. Test SQLite backup/restore to a separate database and document recovery objectives.
6. Perform a restricted runtime staging validation, including readiness, authorization, OPA failure, budget exhaustion, and TLS verification.

## Required before production

Define a reviewed privacy model/accountant before making formal privacy claims; complete threat modeling, monitoring and retention/log safeguards, measured capacity planning, target-cluster storage validation, and independent security/privacy review. Establish a supported shared budget design before scaling beyond one app replica.

## Verdict

**NOT YET READY FOR STAGING ROLLOUT OR PRODUCTION.** Identity and TLS controls are now implemented in code/configuration, but the changes and target deployment environment still need verification and operational secrets/certificates.
