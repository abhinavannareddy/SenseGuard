# Security and privacy review

Date: 2026-10-17. This is a source/configuration review, not a penetration test. Use simulated data only.

## Confirmed controls

- OPA authorization runs before budget charging. OPA transport failures, timeouts, non-200 responses, malformed JSON, and missing decisions fail closed.
- SQLite statements are parameterized. Budget updates are transactional, validate finite positive costs, and return data only after a confirmed commit.
- The app container is non-root, read-only-root, and configured with `no-new-privileges`; its writable database directory is a named volume. Published Compose ports bind to loopback.
- `/readyz` checks the database and OPA HTTP `/health`. `/healthz` is a liveness response.
- The SQLite ledger is designed for one SenseGuard replica. POSIX file permissions are restricted where supported; Windows ACL and Kubernetes storage behavior are environment-dependent.
- Coarse mode rounds coordinates and redacts IDs. Aggregate mode returns counts. Noisy mode adds pseudo-random Laplace-shaped noise.

Previously recorded tests include Go tests, race tests, vet, coverage (72.3%), OPA policy tests (11/11), Compose validation, container build and local runtime checks. Those historical results are described in `VERIFICATION_REPORT.md`; they were not rerun in this session because Go and OPA are unavailable and Docker Engine access was denied.

## Critical findings and limits

- **Authentication is absent.** `/sensing` and `/budget` derive the consumer from caller-controlled `X-Consumer-ID`. Anyone able to reach the service can impersonate a consumer, spend their budget, and retrieve their balance. OPA's allow-list is not identity verification. There is no credential issuer or established secret configuration in this prototype, so this issue is unresolved. Do not expose the service beyond a controlled local environment.
- **Transport security is absent.** The API and OPA use HTTP. Compose loopback binding limits host exposure but does not provide TLS or protect traffic through a proxy/host compromise. Production needs verified identity, TLS, and appropriate network controls, including pod-to-pod OPA traffic.
- **OPA health check is weak.** Compose's current OPA health check evaluates the constant `true`; it can report healthy without checking the listener or policy. SenseGuard readiness checks database and OPA `/health`, but this health endpoint does not prove the intended policy is loaded. OPA unavailability during authorization fails closed.
- **No formal differential privacy.** The budget is a per-request counter, not epsilon accounting. Noise has no defined sensitivity, neighboring-data model, or accountant. Coarse output retains types/counts, and small aggregates can expose individuals. No formal differential privacy claim is supported.
- **Backup/recovery is unverified.** The app-stopped file-copy instructions are not a tested recovery procedure. The persistent volume is not a backup.
- **Kubernetes is unverified.** No cluster deployment or reliable offline schema validation was completed in this session. StorageClass, volume permissions, `fsGroup`, image availability, and persistence require target validation. The deployment is single replica.
- SQLite is not application-encrypted. Logs can include consumer IDs and purposes.

## Required before staging with untrusted callers or production

1. Implement verified consumer authentication and bind both sensing and budget lookup to the authenticated principal; test unauthenticated and impersonation attempts.
2. Configure TLS at a reviewed endpoint and protect internal service traffic; restrict network access.
3. Improve the OPA health check and verify startup/readiness behavior with the selected OPA image.
4. Define and review formal privacy/accounting semantics before making privacy guarantees.
5. Test a backup and restore into a separate database location; establish retention and recovery objectives.
6. Validate Kubernetes manifests offline and validate storage, permissions, images, and persistence in the target environment.
7. Complete threat modeling, monitoring/log review, and independent security/privacy review.

## Verdict

**NOT READY FOR STAGING WITH UNTRUSTED CALLERS OR PRODUCTION DEPLOYMENT.** Local restricted development only. No authentication, TLS, formal privacy model, or validated recovery process is in place.
