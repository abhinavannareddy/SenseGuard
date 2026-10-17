# SenseGuard

SenseGuard is a learning project for privacy-aware release of **simulated** 6G sensing data. Its Go API checks authorization with Open Policy Agent (OPA), applies a privacy mode, then charges a per-consumer budget in SQLite.

This is a prototype. It is not production-ready and does not provide formal differential privacy.

## How a request works

1. A client sends an HTTP request to SenseGuard on port 8080.
2. SenseGuard validates the consumer ID, purpose, and mode.
3. It asks OPA on port 8181 whether the request is allowed. OPA errors, timeouts, non-200 responses, or malformed replies fail closed.
4. For an allowed request, SenseGuard prepares coarse, noisy, or aggregate output.
5. A SQLite transaction deducts the configured cost. Protected data is returned only after the transaction is confirmed.

Go handles HTTP, transformations, and the ledger. OPA evaluates `policy/senseguard.rego`. Docker Compose runs both locally. Kubernetes files describe a cluster setup but do not deploy it.

## Configuration

| Variable | Default / container value | Purpose |
| --- | --- | --- |
| `OPA_URL` | localhost:8181 / http://opa:8181 | OPA server |
| `DATABASE_PATH` | data/senseguard.db / /data/senseguard.db | SQLite file |
| `DEFAULT_PRIVACY_BUDGET` | 5 | Initial balance for a new consumer |
| `REQUEST_PRIVACY_COST` | 1 | Cost for each authorized data response |
| `COARSE_GRID_SIZE` | 10 | Coordinate rounding grid |
| `NOISE_SCALE` | 3 | Demonstration noise scale |

Invalid or non-positive numeric environment values use defaults.

## Run directly with Go

This needs Go installed and OPA running. In a first PowerShell window, start OPA (the first run may download its official image):

```powershell
docker run --rm -p 127.0.0.1:8181:8181 --mount "type=bind,source=$($PWD.Path)\policy,target=/policy,readonly" openpolicyagent/opa:1.0.0 run --server --addr=0.0.0.0:8181 /policy/senseguard.rego
```

In a second window, from the project directory:

```powershell
$env:OPA_URL = 'http://localhost:8181'
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
go test ./...
go test -race ./...
go vet ./...
go test -cover ./...
opa test policy/ -v
docker compose config --quiet
```

Go tests use a mocked OPA server; `opa test` checks the actual Rego files. Compose config validation does not start services. If using Docker for Go/OPA checks, official images may be downloaded when missing. Ask before pulling images. Read-only mount examples:

```powershell
docker run --rm --mount "type=bind,source=$($PWD.Path),target=/workspace,readonly" --workdir /workspace golang:1.24.13-alpine go test ./...
docker run --rm --mount "type=bind,source=$($PWD.Path),target=/workspace,readonly" --workdir /workspace golang:1.24.13-alpine go vet ./...
docker run --rm --mount "type=bind,source=$($PWD.Path)\policy,target=/policy,readonly" openpolicyagent/opa:1.0.0 test /policy -v
```

These need a working Docker engine and available images. Check `VERIFICATION_REPORT.md` for what actually ran.

## Start with Docker Compose

```powershell
docker compose up --build
```

This builds the API image, may download Go, Distroless, and OPA images, and starts both services. Both published ports bind to localhost. Stop with Ctrl+C, or use `docker compose stop` / `docker compose down` without `-v`.

Compose waits for the OPA container health check before starting SenseGuard. The current OPA health check only evaluates the constant expression `true`; it does not verify the HTTP listener or the loaded policy. SenseGuard `/readyz` separately checks SQLite and OPA HTTP `/health`, and policy errors fail closed for data requests. Do not interpret Compose dependency health as proof that policy is loaded.

Health and readiness:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

`/healthz` checks that the HTTP process responds. `/readyz` checks both SQLite and OPA. The Docker image's built-in health check calls `/readyz`, so the container is healthy only when the service can reach both dependencies.

## Try the API and inspect a budget

**Local demonstration only:** `X-Consumer-ID` is caller-controlled and is not authentication. Anyone who can reach this HTTP service can impersonate another consumer and inspect or consume that consumer's budget. Compose binds ports to localhost for local use, but this API must not be exposed to an untrusted network. Production use requires verified identities and credentials, identity-bound authorization, TLS, and protection of `/budget` as well as `/sensing`.

Allowed coarse response:

```powershell
Invoke-RestMethod -Headers @{'X-Consumer-ID'='researcher'} 'http://localhost:8080/sensing?purpose=crowd-analysis&mode=coarse'
```

Allowed aggregate response:

```powershell
Invoke-RestMethod -Headers @{'X-Consumer-ID'='mall-manager'} 'http://localhost:8080/sensing?purpose=occupancy-monitoring&mode=aggregate'
```

Denied request (HTTP 403):

```powershell
try { Invoke-RestMethod -Headers @{'X-Consumer-ID'='researcher'} 'http://localhost:8080/sensing?purpose=track-person&mode=coarse' } catch { $_.Exception.Response.StatusCode }
```

Budget lookup:

```powershell
Invoke-RestMethod -Headers @{'X-Consumer-ID'='researcher'} 'http://localhost:8080/budget'
```

With the defaults, five successful requests use the five-unit budget; the next allowed request returns HTTP 429. A denied request does not charge.

## Check persistence after app-container recreation

Record a caller's current balance, recreate only the API container, then query again:

```powershell
docker compose up -d --force-recreate senseguard
Invoke-RestMethod -Headers @{'X-Consumer-ID'='researcher'} 'http://localhost:8080/budget'
```

The named volume remains attached. Compare the balance before and after. This command has not been run as part of this verification.

## Back up the database

This stopped-service file-copy procedure is a basic offline copy, not a verified backup/recovery process. Stop the API to avoid writes, copy its DB file, then start it again:

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

Logs omit sensing coordinates/data, but may include caller-controlled consumer IDs and purposes. Do not put secrets there.

- `policy service unavailable` (503): OPA is unreachable, starting, or returned an invalid response.
- `privacy budget service unavailable` (503): check that the database volume is mounted and writable.
- `privacy budget exhausted` (429): the caller has no remaining balance.
- Docker named-pipe access denied: Docker may work in a regular terminal while this session cannot access its engine. Use an authorized Windows terminal; do not change pipe permissions casually.
- Build/pull errors: images or Go modules may need downloads. Confirm before allowing downloads.

## Kubernetes (preparation only)

Files in `k8s/` define namespace, policy ConfigMap, internal OPA Deployment/Service, a 1 GiB PVC, and SenseGuard Deployment/Service. SenseGuard uses one replica, the DB at `/data/senseguard.db`, /readyz and /healthz probes, resource requests/limits, and non-root controls. OPA remains ClusterIP/internal.

The PVC leaves `storageClassName` unset, so the cluster needs a default dynamic StorageClass. The storage driver must support the configured pod group (`fsGroup: 65532`). Confirm image availability and backup/recovery before deployment.

Future commands only — **NOT EXECUTED** (they change a cluster):

```powershell
kubectl apply -f .\k8s\namespace.yaml
kubectl apply -f .\k8s\opa-config.yaml
kubectl apply -f .\k8s\opa-deployment.yaml
kubectl apply -f .\k8s\senseguard-pvc.yaml
kubectl apply -f .\k8s\senseguard-deployment.yaml
kubectl -n senseguard rollout status deployment/opa
kubectl -n senseguard rollout status deployment/senseguard
kubectl -n senseguard port-forward svc/senseguard 8080:8080
```

See `DEPLOYMENT_CHECKLIST.md` first. No Kubernetes deployment was performed.

## Privacy and security limitations

- `X-Consumer-ID` is caller-supplied, not authentication. A caller can spoof identities. The 10,000 limit bounds records but does not prevent identity abuse.
- SQLite transactions/mutex protect this single replica; they do not provide a shared budget across multiple app replicas.
- Budget units are per-request counters, not epsilon accounting.
- Coarse mode rounds coordinates and redacts IDs, but keeps object types and counts.
- Noisy mode adds pseudo-random Laplace-shaped noise, but no sensitivity, neighboring-data model, or privacy accountant is defined. Noise alone is not differential privacy.
- Aggregate counts can expose information about small groups.
- Use simulated data only. Real deployment needs identity, TLS, network controls, retention and log policies, monitoring, tested restore, and expert security/privacy review.

## Project map

- `cmd/senseguard/main.go`: API, authorization, transformations, startup/shutdown.
- `cmd/senseguard/ledger_sqlite.go`: SQLite schema and budget transactions.
- `cmd/senseguard/main_test.go`: API and persistence tests.
- `policy/`: OPA rules and tests.
- `Dockerfile`, `docker-compose.yml`: local container configuration.
- `k8s/`: cluster resources and persistent storage.
- `DEPLOYMENT_CHECKLIST.md`, `SECURITY_PRIVACY_REVIEW.md`, `VERIFICATION_REPORT.md`: review material.
