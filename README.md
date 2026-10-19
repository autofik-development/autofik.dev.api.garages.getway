# autofik.dev.api.garages.getway

Garages gateway: a Go gRPC server with a built-in grpc-gateway REST/JSON
transcoder, in front of the garages CMS
([autofik.dev.api.garages.cms](https://github.com/autofik-development/autofik.dev.api.garages.cms)).
Every RPC is one call to the CMS's GraphQL API, covering garages, their service
catalog and their staff.

| Listener | Env var               | Default  |
| -------- | --------------------- | -------- |
| gRPC     | `GRPC_LISTEN_ADDR`    | `:50057` |
| REST     | `GATEWAY_LISTEN_ADDR` | `:8083`  |

| Upstream          | Env var                  | Default                         |
| ----------------- | ------------------------ | ------------------------------- |
| Garages CMS (GQL) | `GARAGE_CMS_SERVICE_URL` | `http://localhost:4042/graphql` |

## Routes

| REST                                                   | CMS GraphQL                    |
| ------------------------------------------------------ | ------------------------------ |
| `GET /garages/v1/garages`                              | `garages`                      |
| `GET /garages/v1/garages/{id}`                         | `garage` (+ services, staff)   |
| `POST /garages/v1/garages`                             | `createGarage`                 |
| `PATCH /garages/v1/garages/{id}`                       | `updateGarage`                 |
| `PATCH /garages/v1/garages/{id}/status`                | `setGarageStatus`              |
| `PATCH /garages/v1/garages/{id}/verified`              | `setGarageVerified`            |
| `DELETE /garages/v1/garages/{id}`                      | `deleteGarage`                 |
| `GET /garages/v1/garage-services`                      | `garageServices`               |
| `GET /garages/v1/garage-services/count`                | `garageServicesCount`          |
| `GET /garages/v1/garage-services/{id}`                 | `garageService`                |
| `POST /garages/v1/garage-services`                     | `createGarageService`          |
| `PATCH /garages/v1/garage-services/{id}`               | `updateGarageService`          |
| `PATCH /garages/v1/garage-services/{id}/availability`  | `setGarageServiceAvailability` |
| `DELETE /garages/v1/garage-services/{id}`              | `deleteGarageService`          |
| `GET /garages/v1/staff-members`                        | `staffMembers`                 |
| `GET /garages/v1/staff-members/{id}`                   | `staffMember`                  |
| `POST /garages/v1/staff-members`                       | `createStaff`                  |
| `PATCH /garages/v1/staff-members/{id}`                 | `updateStaff`                  |
| `DELETE /garages/v1/staff-members/{id}`                | `deleteStaff`                  |

The resources and verbs match the routes the booking one-gateway already
publishes for this CMS (`/api/v1/garages`, `/api/v1/garage-services`,
`/api/v1/staff-members`). Field names and the response envelope follow the Go
gateways instead.

Full request and response fields, with allowed values, are documented in
[`garages_gateway.proto`](internal/grpc/proto/garages/v1/garages_gateway.proto).

```bash
curl -X POST localhost:8083/garages/v1/garages -H 'Content-Type: application/json' -d '{
  "garages_admin_id": "9a1c2b3d-4e5f-4a6b-8c7d-0e1f2a3b4c5d",
  "garage_name": "Autofik Auto Care", "address_location": "12 Street 271, Phnom Penh",
  "latitude": "11.5564", "longitude": "104.9282",
  "days": ["MONDAY", "TUESDAY"], "open_time": "08:00", "close_time": "17:30"}'

curl 'localhost:8083/garages/v1/garages?status=ACTIVE&is_verified=true&limit=10'
curl -X PATCH localhost:8083/garages/v1/garages/{id}/status \
  -H 'Content-Type: application/json' -d '{"status":"ACTIVE"}'
curl 'localhost:8083/garages/v1/garage-services/count?garage_id={id}'
```

### Conventions

- **Envelope.** Every response is `{status, message, code, data}`, with
  snake_case keys. Every field is always present, so the shape is stable.
- **Nulls.** A string the CMS returns as null comes back as `""`. A nullable
  integer (`experience_years`, `duration_minutes`) stays `null`, because `0`
  is a real value there.
- **Enums are strings.** Values are passed through in the CMS's own form
  (`ACTIVE`, `MONDAY`, `MAINTENANCE`, …), so a new CMS value needs no gateway
  release. Input is case-insensitive. `is_insured` and `is_available` are the
  CMS's `"TRUE"`/`"FALSE"` flags; `is_verified` is a boolean.
- **Coordinates are strings** (`"11.5564"`), as in the CMS schema.
- **PATCH changes only the fields sent.** A field sent as `0`, `false` or `""`
  is applied. An empty `days` list leaves the stored days unchanged.
- **Lists are flat.** They leave out each row's relations, which would cost
  the CMS a query per row: `garage` is `null` on list rows. `GET` by id returns
  them: a garage with its `services` and `staff`, or a service or staff
  member with its `garage`.
- **Paging.** List endpoints take `limit` (default 25, max 100) and `offset`.

### Errors

Errors use the platform envelope `{status: "error", message, code}`:

| Cause                                                  | HTTP | `code`                |
| ------------------------------------------------------ | ---- | --------------------- |
| Malformed id or missing required field (gateway)       | 400  | `INVALID_ARGUMENT`    |
| CMS validation or business rule, unknown enum value    | 400  | `INVALID_ARGUMENT`    |
| Garage, service or staff member not found              | 404  | `NOT_FOUND`           |
| Garage already offers that service category            | 409  | `ALREADY_EXISTS`      |
| CMS unreachable, or answered 502/503                   | 503  | `SERVICE_UNAVAILABLE` |
| CMS timed out                                          | 504  | `DEADLINE_EXCEEDED`   |
| Any other CMS failure                                  | 500  | `INTERNAL_ERROR`      |

4xx messages come from the CMS and are written for the caller ("Garage not
found", "closeTime must be later than openTime"). 5xx messages are replaced
with a generic one, because they can carry internal detail; the original is
logged.

Nest's Apollo driver reports a `NotFoundException` or `ConflictException` as
`INTERNAL_SERVER_ERROR`, with the real status only in
`extensions.originalError.statusCode`. `internal/cms` maps from that status.
Mapping from the GraphQL code would turn every "not found" into a 500.

## Auth

This gateway does not authenticate callers, and neither does the CMS behind
it. That matches the admin gateway: both rely on the edge (APISIX), so do not
expose either listener directly.

The CMS reads `x-mysabay-user-id` and `x-hash`/`x-signature`/`x-timestamp`
but does not verify them. The gateway deliberately forwards none of them,
since a caller could set them to anything. Only `X-Request-Id` is passed on,
for log correlation.

## Health endpoints

| Endpoint                         | Kind      | Notes                                                        |
| -------------------------------- | --------- | ------------------------------------------------------------ |
| `GET /healty`, `GET /healthz`    | Liveness  | Always 200 while the process is up, including during drain.  |
| `GET /readyz`                    | Readiness | 503 `HEALTH_CHECK_DRAINING` once shutdown starts.            |
| `GET /garages/v1/health`         | REST→gRPC | Transcoded to `garages.v1.GaragesGatewayService/HealthCheck`. |
| `grpc.health.v1.Health/Check`    | gRPC      | Standard health protocol; service `""` or `garages-gateway`. |

None of them probe the CMS.

Probe body (`/healty`, `/healthz`, `/readyz`):

```json
{"status":"success","message":"ok","code":"HEALTH_CHECK_SUCCESS","serive":"garages getway","ping-code":"AA001"}
```

The image is distroless, so the container healthcheck is the binary itself:
`garages-gateway -healthcheck` checks the gRPC health status over loopback
and exits 0 when the server reports `SERVING`.

## Run

```bash
make run                          # go run ./cmd
curl localhost:8083/healty
curl localhost:8083/garages/v1/garages

docker compose up -d --build      # same, in a container; reaches a host CMS
                                  # through host.docker.internal
```

For `grpcurl`, start with `GRPC_ENABLE_REFLECTION=true`:

```bash
grpcurl -plaintext -d '{"limit": 5}' localhost:50057 garages.v1.GaragesGatewayService/ListGarages
```

## Develop

```bash
make proto    # regenerate internal/grpc/pb/garages/<version> from internal/grpc/proto
make check    # gofmt, go vet, go test -race
```

`make proto` needs `protoc` plus `protoc-gen-go`, `protoc-gen-go-grpc` and
`protoc-gen-grpc-gateway` in `$(go env GOPATH)/bin`.

To add a route:

1. Add the RPC with a `google.api.http` option to
   `internal/grpc/proto/garages/v1/garages_gateway.proto` and run `make proto`.
2. Add its GraphQL document and client method to `internal/cms/operations.go`.
3. Implement it on `GaragesGatewayServer` in `internal/grpc/service`.

When two patterns overlap, declare the literal one after the `{id}` one:
grpc-gateway gives the last-registered pattern precedence. That is why
`CountGarageServices` comes after `GetGarageService` in the proto.

`test/schema_test.go` validates every GraphQL document, and the variables the
Go input types encode to, against `test/testdata/cms.graphql`, a snapshot of
the CMS schema. When the CMS schema changes, refresh the snapshot with
[`scripts/print-cms-schema.ts`](scripts/print-cms-schema.ts). It builds the
schema from the CMS's resolvers without booting the CMS; its header has the
command.

The API is versioned in three places that move together: the proto package
(`garages.v1`), its directory (`internal/grpc/proto/garages/v1/`, generated
into `internal/grpc/pb/garages/v1/`), and the REST prefix (`/garages/v1/`). A
breaking change goes in a new `garages/v2/` package beside v1 rather than
editing v1; `make proto` picks up every `.proto` under `internal/grpc/proto`.

## Layout

```
cmd/main.go                      servers, health registration, graceful shutdown
internal/cms/                    GraphQL client for the garages CMS, error mapping
internal/gateway/                grpc-gateway handler, REST mux, probe handlers
internal/grpc/proto/garages/v1/  .proto sources (package garages.v1)
internal/grpc/pb/garages/v1/     generated code (do not edit)
internal/grpc/service/           GaragesGatewayService implementation
internal/interceptors/           recovery, logging, timeout
pkg/googleapis/                  google/api annotations for protoc
scripts/print-cms-schema.ts      prints the CMS schema for the test snapshot
test/                            integration tests over real gRPC/HTTP
```
