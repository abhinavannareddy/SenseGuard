# SenseGuard deployment checklist

This checklist records repository verification only. No staging, production, cloud, or Kubernetes deployment has been performed. The current verdict remains **A. NOT READY FOR STAGING**.

## Verification of the current working tree, 2026-10-19

- [x] `go mod verify` passed with Go 1.26.9; all modules verified.
- [x] `go test ./cmd/senseguard/...` passed with Go 1.26.9.
- [x] `go vet ./cmd/senseguard/...` passed without diagnostics.
- [x] `go test -race ./cmd/senseguard/...` passed with Go 1.26.9.
- [x] `go test -cover ./cmd/senseguard/...` passed, 70.5% statement coverage.
- [x] `govulncheck` v1.1.4 reported no known vulnerabilities for the final Go 1.26.9 toolchain and module set. Go 1.24.13 findings prompted the toolchain and `golang.org/x/sys` upgrade.
- [x] OPA 1.0.0 policy tests passed, 16/16; the pinned image supports `--disable-telemetry`.
- [x] `docker compose config --quiet` passed with a non-production temporary token map.
- [x] `docker build -t senseguard:verify-20261019-go1269 .` passed.
- [x] `kubectl kustomize .` rendered the root Kustomize set, generating its ConfigMap from the tested policy source.
- [x] Strict Kubeconform schema validation passed: 9 resources valid, 0 invalid. No cluster was contacted.
- [x] `actionlint` passed for `.github/workflows/verify.yml`.
- [x] Isolated runtime checks passed for authentication, identity mismatch, authorization, budget charging/exhaustion, online snapshot verification, readiness, and fail-closed OPA outage behavior. They used a unique temporary Docker network and a tmpfs database; no existing volume or database was mounted.
- [x] Go tests created a SQLite snapshot, verified integrity, refused to overwrite an existing destination, and restored a separate copy with the expected balance.
- [x] Backup CLI rejects a non-SenseGuard database without changing its contents.
- [x] The Go mTLS integration test passed with ephemeral local certificates; deployment certificates and target trust configuration are not covered by this test.
- [x] The first GitHub-hosted Actions run passed for commit `8e65fe5`.
- [x] The updated workflow passed on commit `77a47e0` after pinning action SHAs and Ubuntu 24.04.
- [ ] Target-issued certificates, trust chains, renewal, DNS SANs, and target-cluster TLS/mTLS behavior remain unverified.
- [ ] Target Kubernetes version, cluster CNI, certificates, credentials, registry image, PVC behavior, and operational recovery objectives remain unverified.

## Before local Compose use

- [ ] Use simulated data only.
- [ ] Confirm required software/image downloads are allowed.
- [ ] Run `docker compose config --quiet` with a temporary token map.
- [ ] Start with `docker compose up --build`. This profile uses HTTP and binds to localhost; do not expose it to untrusted networks.
- [ ] Check `/healthz`, `/readyz`, allowed/denied requests, and `/budget`.
- [ ] Recreate only the SenseGuard container and verify its balance persists.
- [ ] Create an online SQLite snapshot, export it to independent protected storage, and perform a separate-location restore test.

## Before any Kubernetes deployment

- [x] Render the full set with `kubectl kustomize .`.
- [x] Validate the rendered standard resource schemas with strict Kubeconform.
- [ ] Validate against the actual target Kubernetes version and admission policies.
- [ ] Confirm the namespace is `senseguard` and OPA remains internal ClusterIP.
- [ ] Confirm a default StorageClass exists, or select an approved class for the 1 GiB PVC.
- [ ] Confirm the CSI driver supports `fsGroup: 65532`, correct ownership, and persistence.
- [ ] Publish the reviewed application image to an approved registry and pin the deployment to its immutable digest; the manifest currently uses `senseguard:local`.
- [ ] Keep SenseGuard at one replica with this SQLite layout.
- [ ] Have the cluster operator label only approved client namespaces with `senseguard.io/allow-senseguard=true` and client pod templates with `senseguard.io/client=true`. Verify that the CNI enforces both NetworkPolicies and review any other policies that could allow additional ingress.
- [ ] Issue API, OPA server, OPA client, and CA certificates from the approved PKI. Include required DNS SANs, pin the OPA client subject to exactly `CN=senseguard-api`, and establish renewal/rotation.
- [ ] Create the three TLS Secrets and `senseguard-consumer-credentials` from protected files. Verify Secret encryption at rest, least-privilege RBAC, issuance, revocation, and rotation for static bearer tokens.
- [ ] Test TLS/mTLS, authentication, denial, OPA outage, budget exhaustion, probes, storage persistence, and backup restore in the target environment.
- [ ] Establish external access only through a reviewed TLS-preserving gateway and approved network controls. No Ingress is included.

## Before restricted staging

- [ ] Confirm the GitHub Actions run passes for the exact release commit.
- [ ] Complete target-cluster and certificate checks above.
- [ ] Configure independent automated backups, retention, access controls, and recovery objectives; complete a restore drill on separate storage.
- [ ] Add monitoring, log retention/redaction, rate limiting at the approved gateway, capacity measurements, rollback, and incident procedures.
- [ ] Complete threat modeling and independent security/privacy review.
- [ ] Keep data simulated unless a reviewed privacy model, legal basis, retention plan, and production data source have been approved.

## Remaining production blockers

The repository now has automated verification configured, stricter Kubernetes ingress, source-generated policy ConfigMaps, startup probes, and a tested SQLite snapshot path. Production still depends on target-specific credentials, PKI, image publication, network/storage policy enforcement, recovery objectives, monitoring and operational procedures, independent review, and an approved privacy model for any real sensing data. The noisy mode is not a formal differential privacy guarantee. SQLite supports one SenseGuard replica only.
