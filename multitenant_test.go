package multitenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeaderResolver(t *testing.T) {
	res := Header("X-Tenant-ID")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := res.Resolve(req); !errors.Is(err, ErrNoTenant) {
		t.Errorf("empty header: err=%v, want ErrNoTenant", err)
	}
	req.Header.Set("X-Tenant-ID", "  acme  ")
	id, err := res.Resolve(req)
	if err != nil || id != "acme" {
		t.Errorf("got %q, %v; want trimmed acme", id, err)
	}
}

func TestSubdomainResolver(t *testing.T) {
	res := Subdomain("example.com")
	cases := []struct {
		host string
		want string
	}{
		{"acme.example.com", "acme"},
		{"acme.example.com:8080", "acme"},
		{"ACME.EXAMPLE.COM", "acme"},
		{"acme.example.com.", "acme"},
		{"example.com", ""},
		{"www.example.com", ""},
		{"example.com:8080", ""},
		{"evil.com", ""},
		{"a.b.example.com", ""}, // nested subdomains are not tenants
		{"localhost:8080", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = c.host
		id, err := res.Resolve(req)
		if c.want == "" {
			if !errors.Is(err, ErrNoTenant) {
				t.Errorf("host %q: got %q, want ErrNoTenant", c.host, id)
			}
			continue
		}
		if err != nil || id != c.want {
			t.Errorf("host %q: got %q, %v; want %q", c.host, id, err, c.want)
		}
	}
}

func TestPathResolver(t *testing.T) {
	res := Path()
	cases := []struct {
		path string
		want string
	}{
		{"/acme/orders", "acme"},
		{"/acme", "acme"},
		{"/", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		id, err := res.Resolve(req)
		if c.want == "" {
			if !errors.Is(err, ErrNoTenant) {
				t.Errorf("path %q: got %q, want ErrNoTenant", c.path, id)
			}
			continue
		}
		if err != nil || id != c.want {
			t.Errorf("path %q: got %q, %v", c.path, id, err)
		}
	}
}

func TestChainResolver(t *testing.T) {
	res := Chain(Header("X-Tenant-ID"), Static("fallback"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	id, err := res.Resolve(req)
	if err != nil || id != "fallback" {
		t.Errorf("got %q, %v; want fallback", id, err)
	}
	req.Header.Set("X-Tenant-ID", "acme")
	id, err = res.Resolve(req)
	if err != nil || id != "acme" {
		t.Errorf("got %q, %v; want acme (first wins)", id, err)
	}
}

func TestMiddleware_StoresTenant(t *testing.T) {
	h := Middleware(Static("acme"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := FromContext(r.Context())
		if !ok {
			t.Error("no tenant in context")
			return
		}
		if tenant.ID != "acme" {
			t.Errorf("ID=%q", tenant.ID)
		}
		if TenantID(r.Context()) != "acme" {
			t.Error("TenantID shortcut wrong")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code=%d", rec.Code)
	}
}

func TestMiddleware_UnresolvableIs400(t *testing.T) {
	called := false
	h := Middleware(Header("X-Tenant-ID"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code=%d, want 400", rec.Code)
	}
	if called {
		t.Error("handler ran despite unresolvable tenant")
	}
}

func TestMiddleware_FinderEnriches(t *testing.T) {
	finder := FinderFunc(func(ctx context.Context, id string) (Tenant, error) {
		if id == "acme" {
			return Tenant{ID: "acme", Name: "Acme Corp"}, nil
		}
		return Tenant{}, errors.New("unknown tenant")
	})
	h := Middleware(Header("X-Tenant-ID"), WithFinder(finder))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if MustFromContext(r.Context()).Name != "Acme Corp" {
			t.Error("finder did not enrich tenant")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "acme")
	h.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Tenant-ID", "ghost")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req2)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("finder error: code=%d, want 400", rec.Code)
	}
}

func TestMiddleware_CustomErrorHandler(t *testing.T) {
	h := Middleware(Header("X-Tenant-ID"),
		WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "custom", http.StatusTeapot)
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("code=%d, want 418", rec.Code)
	}
}
