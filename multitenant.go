// Package multitenant resolves the current tenant from an HTTP request and
// carries it through the request context.
//
// Tenancy usually leaks into every layer: parsing subdomains in handlers,
// threading tenant IDs through function signatures, remembering the WHERE
// clause on every query. This package centralizes the first two:
//
//	resolver := multitenant.Chain(
//		multitenant.Header("X-Tenant-ID"),
//		multitenant.Subdomain("example.com"),
//	)
//	http.ListenAndServe(":8080", multitenant.Middleware(resolver)(router))
//
// Downstream, tenant, ok := multitenant.FromContext(r.Context()).
// For GORM query scoping, see the gormscope subpackage.
package multitenant

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
)

// Tenant is the resolved tenant for a request. ID is the canonical,
// storage-level identifier (the value your tenant_id columns hold).
// Resolvers only fill ID; use WithFinder to enrich the rest.
type Tenant struct {
	ID   string
	Name string
}

// Resolver extracts a tenant ID from a request.
type Resolver interface {
	// Resolve returns the tenant ID or an error when it cannot determine one.
	Resolve(r *http.Request) (string, error)
}

// ErrNoTenant is returned by resolvers that find nothing.
var ErrNoTenant = errors.New("multitenant: no tenant could be resolved")

// ResolverFunc adapts a function to a Resolver.
type ResolverFunc func(r *http.Request) (string, error)

// Resolve implements Resolver.
func (f ResolverFunc) Resolve(r *http.Request) (string, error) { return f(r) }

// Header resolves the tenant from a request header. Empty header value
// means ErrNoTenant.
func Header(name string) Resolver {
	return ResolverFunc(func(r *http.Request) (string, error) {
		if v := strings.TrimSpace(r.Header.Get(name)); v != "" {
			return v, nil
		}
		return "", ErrNoTenant
	})
}

// Subdomain resolves the tenant from the first DNS label of the host:
// "acme.example.com" with base "example.com" resolves "acme".
// Ports are stripped. The bare base domain ("example.com") and "www"
// resolve to ErrNoTenant.
func Subdomain(base string) Resolver {
	base = strings.ToLower(strings.TrimSuffix(base, "."))
	return ResolverFunc(func(r *http.Request) (string, error) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h // strip :port; bare hosts keep SplitHostPort's error and stay as-is
		}
		host = strings.TrimSuffix(host, ".")
		if host == base || host == "www."+base {
			return "", ErrNoTenant
		}
		if rest, ok := strings.CutSuffix(host, "."+base); ok && rest != "" && !strings.Contains(rest, ".") {
			return rest, nil
		}
		return "", ErrNoTenant
	})
}

// Path resolves the tenant from the first URL path segment:
// "/acme/orders" resolves "acme". The root path resolves ErrNoTenant.
func Path() Resolver {
	return ResolverFunc(func(r *http.Request) (string, error) {
		p := strings.Trim(r.URL.Path, "/")
		if p == "" {
			return "", ErrNoTenant
		}
		seg, _, _ := strings.Cut(p, "/")
		if seg == "" {
			return "", ErrNoTenant
		}
		return seg, nil
	})
}

// Static always resolves the same tenant. Useful for single-tenant
// deployments, local development, and tests.
func Static(id string) Resolver {
	return ResolverFunc(func(r *http.Request) (string, error) { return id, nil })
}

// Chain tries resolvers in order and returns the first success.
// When all fail, it returns the last error.
func Chain(resolvers ...Resolver) Resolver {
	return ResolverFunc(func(r *http.Request) (string, error) {
		var err error = ErrNoTenant
		for _, res := range resolvers {
			var id string
			if id, err = res.Resolve(r); err == nil {
				return id, nil
			}
		}
		return "", err
	})
}

// Finder loads a full Tenant for an ID, e.g. from your tenants table.
type Finder interface {
	FindTenant(ctx context.Context, id string) (Tenant, error)
}

// FinderFunc adapts a function to a Finder.
type FinderFunc func(ctx context.Context, id string) (Tenant, error)

// FindTenant implements Finder.
func (f FinderFunc) FindTenant(ctx context.Context, id string) (Tenant, error) {
	return f(ctx, id)
}

type ctxKey struct{}

// FromContext returns the tenant stored by Middleware.
func FromContext(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(ctxKey{}).(Tenant)
	return t, ok
}

// MustFromContext returns the tenant or panics. Use it in handlers that
// only run behind Middleware.
func MustFromContext(ctx context.Context) Tenant {
	t, ok := FromContext(ctx)
	if !ok {
		panic("multitenant: no tenant in context; is Middleware installed?")
	}
	return t
}

// TenantID is a shortcut for MustFromContext(ctx).ID, the value your
// tenant_id columns and gormscope.Scope want.
func TenantID(ctx context.Context) string { return MustFromContext(ctx).ID }

type config struct {
	finder       Finder
	errorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

// Option configures Middleware.
type Option func(*config)

// WithFinder enriches the resolved ID into a full Tenant before the
// request reaches your handler. Finder errors become errorHandler calls.
func WithFinder(f Finder) Option {
	return func(c *config) { c.finder = f }
}

// WithErrorHandler overrides the default behavior on resolution failure
// (400 Bad Request with a plain message).
func WithErrorHandler(h func(w http.ResponseWriter, r *http.Request, err error)) Option {
	return func(c *config) { c.errorHandler = h }
}

// Middleware resolves the tenant and stores it in the request context.
// Unresolvable requests never reach your handler.
func Middleware(resolver Resolver, opts ...Option) func(http.Handler) http.Handler {
	cfg := &config{
		errorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "tenant could not be resolved", http.StatusBadRequest)
		},
	}
	for _, o := range opts {
		o(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := resolver.Resolve(r)
			if err != nil {
				cfg.errorHandler(w, r, err)
				return
			}
			tenant := Tenant{ID: id}
			if cfg.finder != nil {
				if tenant, err = cfg.finder.FindTenant(r.Context(), id); err != nil {
					cfg.errorHandler(w, r, err)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, tenant)))
		})
	}
}
