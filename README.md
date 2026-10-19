# SenseGuard

SenseGuard is a learning project for privacy-aware release of **simulated** 6G sensing data. Its Go API checks authorization with Open Policy Agent (OPA), applies a privacy mode, then charges a per-consumer budget in SQLite.

This is a prototype. It is not production-ready and does not provide formal differential privacy.

## How a request works

1. A client sends a bearer token over HTTPS to SenseGuard on port 8080 (Kubernetes profile). The localhost-only Compose profile uses HTTP for development.
2. SenseGuard derives consumer identity from the verified bearer token, then validates the purpose and mode. A caller-supplied `X-Consumer-ID` cannot establish identity and is rejected if it conflicts with the token.
3. It asks OPA on port 8181 whether the request is allowed. OPA errors, timeouts, non-200 responses, or malformed replies fail closed.
4. For an allowed request, SenseGuard prepares coarse, noisy, or aggregate output.
5. A SQLite transaction deducts the configured cost. Protected data is returned only after the transaction is confirmed.

Go handles HTTP, transformations, and the ledger. OPA evaluates `policy/senseguard.rego`. Docker Compose runs both locally. Kubernetes files describe a cluster setup but do not deploy it.

## Configuration

| Variable | Default / container value | Purpose |
| --- | --- | --- |
| `OPA_URL` | http://localhost:8181 locally / https://opa:8181 in Kubernetes | OPA server |
| `DATABASE_PATH` | data/senseguard.db / /data/senseguard.db | SQLite file |
| `DEFAULT_PRIVACY_BUDGET` | 5 | Initial balance for a new consumer |
| `REQUEST_PRIVACY_COST` | 1 | Cost for each authorized data response |
| `COARSE_GRID_SIZE` | 10 | Coordinate rounding grid |
| `NOISE_SCALE` | 3 | Demonstration noise scale |
| `CONSUMER_TOKENS` | Required JSON object | Consumer ID to high-entropy bearer token map; keep in a secret store |
| `REQUIRE_TLS` | false locally / true in Kubernetes | Require HTTPS for API and OPA connections |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | unset locally / Secret mount in Kubernetes | API server TLS certificate and key |
| `OPA_CA_FILE` | unset locally / Secret mount in Kubernetes | Trusted OPA server CA |
| `OPA_CLIENT_CERT_FILE`, `OPA_CLIENT_KEY_FILE` | unset locally / Secret mount in Kubernetes | Client certificate for OPA mutual TLS |

Invalid or non-positive numeric environment values use defaults. `REQUIRE_TLS` accepts only `true` or `false`; any other value stops startup. When TLS is enabled, OPA must use HTTPS and its server certificate must chain to the CA configured by `OPA_CA_FILE`. The service exits at startup if `CONSUMER_TOKENS` is missing or malformed. Tokens must be unique and at least 32 characters; generate them with a cryptographically secure random generator and distribute each token only to its consumer.

## Run directly with Go

This needs Go installed and OPA running. In a first PowerShell window, start OPA (the first run may download its official image):

```powershell
docker run --rm -p 127.0.0.1:8181:8181 --mount "type=bind,source=$($PWD.Path)\policy,target=/policy,readonly" openpolicyagent/opa:1.0.0 run --server --addr=0.0.0.0:8181 /policy/senseguard.rego
```

In a second window, from the project directory:

```powershell
$env:OPA_URL = 'http://localhost:8181'
# Set CONSUMER_TOKENS to a JSON map from consumer ID to a unique random token.
# Keep this process environment private; never commit the value.
$env:RESEARCHER_TOKEN = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:CONSUMER_TOKENS = @{ researcher = $env:RESEARCHER_TOKEN } | ConvertTo-Json -Compress
$env:DATABASE_PATH = Join-Path $PWD.Path 'data\senseguard.db'
go run ./cmd/senseguard
```

The default database path is already `data\senseguard.db`; setting it explicitly here shows how to configure it. The folder is created automatically.

## SQLite persistence

SQLite stores one balance per consumer in the `privacy_budgets` table. Each deduction is a transaction, and the existing balance is retained when the app restarts. The ledger refuses new consumers after 10,000 entries. Denied requests do not spend budget. If SQLite cannot confirm a charge, SenseGuard returns HTTP 503 without returning sensing data.

Compose uses a named volume called `senseguard-data` at `/data`. Recreating the app container preserves that volume. Never use `docker compose down -v` if you need to keep its data. Kubernetes uses a 1 GiB PersistentVolumeClaim and one SenseGuard replica. SQLite is not a shared multi-replica ledger.

Persistent storage is not a backup. Protect the volume and practice restoring backups.

## Run checks

Local Go checks require Go 1.24.13 or a compatible newer Go release. Direct Rego checks require OPA 1.0.0 or compatible OPA. Docker commands require Docker Desktop Engine access.

PowerShell commands from the project root:

```powershell
gofmt -w .\cmd\senseguard\*.go
go test ./cmd/senseguard/...
go test -race ./cmd/senseguard/...
go vet ./cmd/senseguard/...
go test -cover ./cmd/senseguard/...
opa test policy/ -v
docker compose config --quiet
```

Go checks target the application package so ignored audit snapshots in the workspace are not included. Go tests use a mocked OPA server; `opa test` checks the actual Rego files. Compose config validation does not start services. If using Docker for Go/OPA checks, official images may be downloaded when missing. Ask before pulling images. Read-only mount examples:

```powershell
docker run --rm --mount "type=bind,source=$($PWD.Path),target=/workspace,readonly" --workdir /workspace golang:1.24.13-alpine go test ./cmd/senseguard/...
docker run --rm --mount "type=bind,source=$($PWD.Path),target=/workspace,readonly" --workdir /workspace golang:1.24.13-alpine go vet ./cmd/senseguard/...
docker run --rm --mount "type=bind,source=$($PWD.Path)\policy,target=/policy,readonly" openpolicyagent/opa:1.0.0 test /policy -v
```

These need a working Docker engine and available images. Check `VERIFICATION_REPORT.md` for what actually ran.

## Start with Docker Compose

```powershell
$env:RESEARCHER_TOKEN = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:CONSUMER_TOKENS = @{ researcher = $env:RESEARCHER_TOKEN } | ConvertTo-Json -Compress
docker compose up --build
```

This builds the API image, may download Go, Distroless, and OPA images, and starts both services. Both published ports bind to localhost. Stop with Ctrl+C, or use `docker compose stop` / `docker compose down` without `-v`.

Compose waits for an OPA health check that calls OPA HTTP `/health` and evaluates a known allowed decision against the mounted policy. SenseGuard `/readyz` separately checks SQLite and OPA `/health`; authorization errors fail closed. The Kubernetes profile uses HTTPS for the API and mutual TLS for OPA.

Health and readiness:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

`/healthz` checks that the HTTP process responds. `/readyz` checks both SQLite and OPA. The Docker image's built-in health check calls `/readyz`, so the container is healthy only when the service can reach both dependencies.

## Try the API and inspect a budget

**Local demonstration only:** Compose uses HTTP and binds to localhost, so do not expose it to an untrusted network. Both protected endpoints require a bearer token mapped to a consumer identity. Keep tokens private; the `X-Consumer-ID` header is not trusted and mismatched claims are rejected. Kubernetes enables HTTPS and mutual TLS to OPA.

Allowed coarse response:

```powershell
Invoke-RestMethod -Headers @{'Authorization'=('Bearer '+$env:RESEARCHER_TOKEN)} 'http://localhost:8080/sensing?purpose=crowd-analysis&mode=coarse'
```

Allowed aggregate response:

```powershell
Invoke-RestMethod -Headers @{'Authorization'=('Bearer '+$env:MALL_MANAGER_TOKEN)} 'http://localhost:8080/sensing?purpose=occupancy-monitoring&mode=aggregate'
```

Denied request (HTTP 403):

```powershell
try { Invoke-RestMethod -Headers @{'Authorization'=('Bearer '+$env:RESEARCHER_TOKEN)} 'http://localhost:8080/sensing?purpose=track-person&mode=coarse' } catch { $_.Exception.Response.StatusCode }
```

Budget lookup:

```powershell
Invoke-RestMethod -Headers @{'Authorization'=('Bearer '+$env:RESEARCHER_TOKEN)} 'http://localhost:8080/budget'
```

With the defaults, five successful requests use the five-unit budget; the next allowed request returns HTTP 429. A denied request does not charge.

## Check persistence after app-container recreation

Record a caller's current balance, recreate only the API container, then query again:

```powershell
docker compose up -d --force-recreate senseguard
Invoke-RestMethod -Headers @{'Authorization'=('Bearer '+$env:RESEARCHER_TOKEN)} 'http://localhost:8080/budget'
```

The named volume remains attached. Compare the balance before and after. This command has not been run as part of this verification.

## Back up the database

Stop the API to avoid writes, copy its DB file, then start it again. This is an offline file copy; backup/restore remains unverified:

```powershell
docker compose stop senseguard
docker compose cp senseguard:/data/senseguard.db .\senseguard.db.backup
docker compose start senseguard
```

Keep the backup protected and out of source control. Restore only to a separate test location and verify it before relying on recovery. Backup/restore has not been validated in this engineering pass.

## Logs and troubleshooting

```powershell
docker compose logs --tail 100 senseguard
docker compose logs --tail 100 opa
```

Logs omit sensing coordinates/data and bearer tokens. They may include authenticated consumer IDs and purposes; avoid placing personal or sensitive values in those fields.

- `policy service unavailable` (503): OPA is unreachable, starting, or returned an invalid response.
- `privacy budget service unavailable` (503): check that the database volume is mounted and writable.
- `privacy budget exhausted` (429): the caller has no remaining balance.
- Docker named-pipe access denied: Docker may work in a regular terminal while this session cannot access its engine. Use an authorized Windows terminal; do not change pipe permissions casually.
- Build/pull errors: images or Go modules may need downloads. Confirm before allowing downloads.

## Kubernetes (preparation only)

Files in `k8s/` define namespace, policy ConfigMap, internal OPA Deployment/Service, a 1 GiB PVC, and SenseGuard Deployment/Service. SenseGuard uses one replica, the DB at `/data/senseguard.db`, /readyz and /healthz probes, resource requests/limits, and non-root controls. OPA remains ClusterIP/internal.

Create the namespace and ConfigMap first, then create Secrets from protected files before applying the deployments. The Kubernetes profile expects these Secret names and keys:

```powershell
kubectl apply -f .\k8s\namespace.yaml
kubectl apply -f .\k8s\opa-config.yaml
kubectl -n senseguard create secret tls senseguard-tls --cert=<api-cert.pem> --key=<api-key.pem>
kubectl -n senseguard create secret generic opa-server-tls --from-file=tls.crt=<opa-server-cert.pem> --from-file=tls.key=<opa-server-key.pem> --from-file=ca.crt=<opa-ca.pem>
kubectl -n senseguard create secret generic senseguard-to-opa-client --from-file=tls.crt=<client-cert.pem> --from-file=tls.key=<client-key.pem> --from-file=ca.crt=<opa-ca.pem>
kubectl -n senseguard create secret generic senseguard-consumer-credentials --from-file=tokens=<protected-token-map.json>
```

The OPA server certificate needs `DNS:opa` and `DNS:opa.senseguard.svc`; the API certificate needs the approved client-facing DNS SAN. The token-map JSON maps consumer IDs to unique random tokens of at least 32 characters. Configure Kubernetes Secret encryption at rest and least-privilege RBAC. These example commands are instructions only and have not been run.

The PVC leaves `storageClassName` unset, so the cluster needs a default dynamic StorageClass. The storage driver must support `fsGroup: 65532`. The app and OPA require externally created TLS Secrets and a consumer-credentials Secret; no secret material is included in this repository. OPA requires a client certificate and validates it against its configured CA. NetworkPolicy restricts OPA ingress to SenseGuard pods, subject to CNI enforcement. The app Service is ClusterIP-only. No ingress is configured.

Future commands only — **NOT EXECUTED** (they change a cluster):

```powershell
kubectl apply -f .\k8s\namespace.yaml
kubectl apply -f .\k8s\opa-config.yaml
kubectl apply -f .\k8s\opa-deployment.yaml
kubectl apply -f .\k8s\senseguard-pvc.yaml
kubectl apply -f .\k8s\senseguard-deployment.yaml
kubectl -n senseguard rollout status deployment/opa
kubectl -n senseguard rollout status deployment/senseguard
kubectl -n senseguard port-forward svc/senseguard 8443:443
```

See `DEPLOYMENT_CHECKLIST.md` first. No Kubernetes deployment was performed.

## Privacy and security limitations

- `/sensing` and `/budget` require a bearer token mapped to a consumer identity at startup. `X-Consumer-ID` cannot establish identity; a mismatch is denied. Protect token delivery, storage, rotation, and revocation.
- SQLite transactions/mutex protect this single replica; they do not provide a shared budget across multiple app replicas.
- Budget units are per-request counters, not epsilon accounting.
- Coarse mode rounds coordinates and redacts IDs, but keeps object types and counts.
- Noisy mode adds pseudo-random Laplace-shaped noise, but no sensitivity, neighboring-data model, or privacy accountant is defined. Noise alone is not differential privacy.
- Aggregate counts can expose information about small groups.
- Use simulated data only. Formal privacy accounting, tested restore, cluster storage validation, monitoring, retention/log policies, and specialist security/privacy review remain required before production.

## Project map

- `cmd/senseguard/main.go`: API, authorization, transformations, startup/shutdown.
- `cmd/senseguard/ledger_sqlite.go`: SQLite schema and budget transactions.
- `cmd/senseguard/main_test.go`: API and persistence tests.
- `policy/`: OPA rules and tests.
- `Dockerfile`, `docker-compose.yml`: local container configuration.
- `k8s/`: cluster resources and persistent storage.
- `DEPLOYMENT_CHECKLIST.md`, `SECURITY_PRIVACY_REVIEW.md`, `VERIFICATION_REPORT.md`: review material.
