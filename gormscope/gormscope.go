// Package gormscope scopes GORM queries to a single tenant.
//
// The rule is simple and absolute: every query in a request touches exactly
// one tenant's rows. The way to guarantee that without relying on memory is
// to make the unscoped query impossible to write by accident:
//
//	db.Scopes(gormscope.For(tenantID)).Find(&orders)
//
// Combine it with multitenant.MustFromContext:
//
//	db.Scopes(gormscope.For(multitenant.TenantID(ctx))).Find(&orders)
//
// Scope applies to reads, updates, and deletes built on the chain. It does
// not set tenant_id on creates: assign TenantID explicitly when inserting,
// so a forgotten assignment fails loudly at the database (NOT NULL) instead
// of silently landing in the wrong tenant.
package gormscope

import (
	"gorm.io/gorm"
)

// Column is the tenant column name. Override it when your schema uses
// something else (e.g. "organization_id") before calling For.
var Column = "tenant_id"

// For returns a GORM scope restricting the query to tenantID's rows.
func For(tenantID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(Column+" = ?", tenantID)
	}
}
