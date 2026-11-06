package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"testing"
	"yuntools/internal/cloud"
)

func TestDomainListPagesAndFiltersBeforePaging(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.page, a.accountID = "dns", "fixture"
	for i := 0; i < 25; i++ {
		a.domains = append(a.domains, cloud.Domain{Name: fmt.Sprintf("site%02d.example.com", i)})
	}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if !tt.HasText("共 25 项") || !tt.HasText("site00.example.com") || tt.HasText("site20.example.com") {
		t.Fatal("first page does not contain exactly first20")
	}
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("site00.example.com") || !tt.HasText("site20.example.com") {
		t.Fatal("next page did not replace rendered rows")
	}
	a.search = "site01"
	tt.Frame()
	if !tt.HasText("site01.example.com") || tt.HasText("site20.example.com") || !tt.HasText("共 1 项") {
		t.Fatal("filter did not reset page and search entire list")
	}
}
