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
- [ ] Run `kubectl kustomize .\k8s` to render and parse the complete manifest set locally.
- [ ] Confirm namespace is senseguard and OPA remains internal ClusterIP.
- [ ] Confirm a default StorageClass exists, or choose an appropriate class for the 1 GiB PVC.
- [ ] Confirm the CSI storage driver supports the configured pod fsGroup and persists data.
- [ ] Make the senseguard:local image available to the cluster or select a reviewed registry image.
- [ ] Keep SenseGuard at one replica with this SQLite layout.
- [ ] Review probes, resource values, non-root settings, and volume ownership against the target.
- [ ] Plan backup, restore, recovery, monitoring, and rollback.
- [ ] Apply only after reviewing the environment and obtaining operational authorization.

## Before a restricted staging rollout

- [ ] Run all required Go and OPA tests, vet, race tests, Compose config validation, and Docker image build against the current source.
- [ ] Validate Kubernetes YAML offline and review the manifests against the target cluster version.
- [ ] Issue API and OPA certificates from the approved CA. Include `DNS:opa` and `DNS:opa.senseguard.svc` on the OPA server certificate; include the service DNS name on the API certificate; set the OPA client certificate subject to exactly `CN=senseguard-api`.
- [ ] Create `senseguard-tls`, `opa-server-tls`, and `senseguard-to-opa-client` Secrets from protected certificate files. Do not commit key files.
- [ ] Create `senseguard-consumer-credentials` with a JSON `tokens` key mapping each consumer to a unique cryptographically random token of at least 32 characters.
- [ ] Confirm Kubernetes Secret encryption at rest and least-privilege RBAC.
- [ ] Confirm the cluster CNI enforces the OPA ingress NetworkPolicy.
- [ ] Verify the target StorageClass, CSI ownership/fsGroup behavior, image availability, and PVC persistence.
- [ ] Test TLS trust, authentication failures, identity mismatch, OPA unavailability, denied requests, budget exhaustion, readiness, and backup restore in the target staging environment.
- [ ] Keep one SenseGuard replica. Do not add an external Ingress until it is separately configured to preserve or re-establish TLS and restrict access.
- [ ] If external access is approved later, add reviewed gateway authentication/rate limiting, TLS handling, network restrictions, and logging controls before exposing the Service.

## Remaining production work

Formal privacy accounting, tested backup/recovery objectives, monitoring and retention controls, capacity planning, target-cluster validation, and independent security/privacy review remain required. Noise alone is not differential privacy. SQLite remains a single-replica design.
