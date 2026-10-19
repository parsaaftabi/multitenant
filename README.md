# multitenant

[![CI](https://github.com/parsaaftabi/multitenant/actions/workflows/ci.yml/badge.svg)](https://github.com/parsaaftabi/multitenant/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/parsaaftabi/multitenant.svg)](https://pkg.go.dev/github.com/parsaaftabi/multitenant)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Tenant resolution and context propagation for Go HTTP services. Resolve the tenant once at the edge (subdomain, header, path), carry it in the request context, and scope every query to it. Plain `net/http`, zero framework lock-in.

```go
resolver := multitenant.Chain(
    multitenant.Header("X-Tenant-ID"),
    multitenant.Subdomain("example.com"),
)

http.ListenAndServe(":8080", multitenant.Middleware(resolver)(router))
```

Downstream:

```go
tenant, ok := multitenant.FromContext(r.Context())
id := multitenant.TenantID(ctx) // panics outside Middleware; use in handlers only
```

## Resolvers

| Resolver | Example |
|---|---|
| `Header("X-Tenant-ID")` | Header value |
| `Subdomain("example.com")` | `acme.example.com` → `acme` (ports stripped, `www` and bare domain rejected) |
| `Path()` | `/acme/orders` → `acme` |
| `Static("acme")` | Fixed tenant: local dev, tests, single-tenant deploys |
| `Chain(a, b, ...)` | First success wins |

Implement `Resolver` (one method) for anything exotic: JWT claims, mTLS CN, lookup tables.

## Enrichment

Resolvers return the storage-level ID. To load the full tenant (name, plan, limits) from your tenants table:

```go
multitenant.Middleware(resolver,
    multitenant.WithFinder(multitenant.FinderFunc(func(ctx context.Context, id string) (multitenant.Tenant, error) {
        return store.FindTenant(ctx, id)
    })),
)
```

Unresolvable requests never reach your handler: `400` by default, or your own `WithErrorHandler`.

## Query scoping (GORM)

The `gormscope` subpackage makes the unscoped query hard to write by accident:

```go
import "github.com/parsaaftabi/multitenant/gormscope"

db.Scopes(gormscope.For(multitenant.TenantID(ctx))).Find(&orders)
```

`For` covers reads, updates, and deletes on the chain. It deliberately does **not** set `tenant_id` on creates — assign it explicitly so a forgotten assignment fails loudly at the database (`NOT NULL`) instead of silently landing in the wrong tenant. Override `gormscope.Column` when your schema names it differently.

## License

MIT — see [LICENSE](LICENSE).
