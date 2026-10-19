package gormscope

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Order struct {
	ID       uint
	TenantID string
	Item     string
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Order{}); err != nil {
		t.Fatal(err)
	}
	seed := []Order{
		{TenantID: "t1", Item: "a"},
		{TenantID: "t1", Item: "b"},
		{TenantID: "t2", Item: "c"},
	}
	for _, o := range seed {
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestFor_ScopesReads(t *testing.T) {
	db := testDB(t)
	var orders []Order
	if err := db.Scopes(For("t1")).Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("got %d orders, want 2", len(orders))
	}
	for _, o := range orders {
		if o.TenantID != "t1" {
			t.Errorf("leaked row from tenant %q", o.TenantID)
		}
	}
}

func TestFor_ScopesUpdatesAndDeletes(t *testing.T) {
	db := testDB(t)

	if err := db.Scopes(For("t1")).Model(&Order{}).Update("Item", "z").Error; err != nil {
		t.Fatal(err)
	}
	var t2 []Order
	db.Scopes(For("t2")).Find(&t2)
	if len(t2) != 1 || t2[0].Item != "c" {
		t.Errorf("scoped update touched another tenant: %+v", t2)
	}

	if err := db.Scopes(For("t1")).Where("item = ?", "z").Delete(&Order{}).Error; err != nil {
		t.Fatal(err)
	}
	var all []Order
	db.Find(&all)
	if len(all) != 1 || all[0].TenantID != "t2" {
		t.Errorf("scoped delete touched another tenant: %+v", all)
	}
}

func TestFor_UnscopedSeesEverything(t *testing.T) {
	db := testDB(t)
	var n int64
	db.Model(&Order{}).Count(&n)
	if n != 3 {
		t.Errorf("unscoped count=%d, want 3 (scope must be opt-in per query)", n)
	}
}

func TestFor_CustomColumn(t *testing.T) {
	old := Column
	Column = "organization_id"
	defer func() { Column = old }()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type Doc struct {
		ID             uint
		OrganizationID string
	}
	if err := db.AutoMigrate(&Doc{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&Doc{OrganizationID: "org1"})

	var docs []Doc
	if err := db.Scopes(For("org1")).Find(&docs).Error; err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Errorf("got %d docs, want 1", len(docs))
	}
}
