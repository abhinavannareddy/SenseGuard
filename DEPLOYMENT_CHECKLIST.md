# SenseGuard deployment checklist

This is preparation guidance. No cluster deployment has been performed.

## Before local Compose use

- [ ] Review `VERIFICATION_REPORT.md`; run checks marked NOT RUN where Go, OPA, and Docker are available.
- [ ] Use simulated data only.
- [ ] Confirm required software/image downloads are allowed.
- [ ] Run `docker compose config --quiet`.
- [ ] Start with `docker compose up --build`.
- [ ] Check /healthz, /readyz, an allowed request, a denied request, and /budget.
- [ ] Recreate only the SenseGuard container and verify the balance persists.
- [ ] Test database backup and restore into a separate location; verify restored balances and integrity.

## Before Kubernetes deployment

- [ ] Validate every YAML file under k8s with an offline YAML/Kubernetes schema validator.
- [ ] Confirm namespace is senseguard and OPA remains internal ClusterIP.
- [ ] Confirm a default StorageClass exists, or choose an appropriate class for the 1 GiB PVC.
- [ ] Confirm the CSI storage driver supports the configured pod fsGroup and persists data.
- [ ] Make the senseguard:local image available to the cluster or select a reviewed registry image.
- [ ] Keep SenseGuard at one replica with this SQLite layout.
- [ ] Review probes, resource values, non-root settings, and volume ownership against the target.
- [ ] Plan backup, restore, recovery, monitoring, and rollback.
- [ ] Apply only after reviewing the environment and obtaining operational authorization.

## Production blockers

The current API trusts the caller-controlled `X-Consumer-ID` header on both `/sensing` and `/budget`. A reachable caller can impersonate another consumer, use their privacy budget, or read their balance. No authentication is implemented. Keep the service restricted to a trusted local development environment; localhost port binding is not a substitute for authentication when using a shared host or proxy.

Before any staging with untrusted callers or production, implement verified consumer identities and credentials, bind authorization and budget lookup to that identity, and test impersonation failures. Configure TLS at a reviewed trusted endpoint and protect service-to-service OPA traffic. Define a formal privacy model before making differential-privacy claims. Test backup and restore, validate Kubernetes manifests and target storage behavior, and complete monitoring and independent security/privacy review.

The Compose OPA health check currently evaluates `true` only. SenseGuard readiness checks SQLite and OPA HTTP `/health`; this does not independently establish that OPA loaded the intended policy. Improve and verify the OPA check before relying on Compose startup health.
