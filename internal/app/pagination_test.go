package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"yuntools/internal/cloud"
)

func TestLocalPaginationClampsAndResetsFilter(t *testing.T) {
	a := &App{}
	start, end := a.pageRange("records", "account-a", 41)
	if start != 0 || end != 20 {
		t.Fatalf("first range %d:%d", start, end)
	}
	a.pages["records"].page = 2
	start, end = a.pageRange("records", "account-a", 41)
	if start != 40 || end != 41 {
		t.Fatalf("last range %d:%d", start, end)
	}
	start, end = a.pageRange("records", "account-a", 3)
	if start != 0 || end != 3 {
		t.Fatalf("shrunk range %d:%d", start, end)
	}
	a.pages["records"].page = 1
	start, end = a.pageRange("records", "account-b", 41)
	if start != 0 || end != 20 {
		t.Fatalf("account change range %d:%d", start, end)
	}
	start, end = a.pageRange("records", "search", 0)
	if start != 0 || end != 0 {
		t.Fatalf("empty range %d:%d", start, end)
	}
}

type pagedOSSFixture struct {
	cloud.ObjectStorage
	mu       sync.Mutex
	requests []string
	blocked  chan struct{}
	entered  chan struct{}
}

func (f *pagedOSSFixture) ListObjectsPage(_ context.Context, bucket, prefix, token string, limit int) (cloud.ObjectPage, error) {
	f.mu.Lock()
	f.requests = append(f.requests, fmt.Sprintf("%s|%s|%s|%d", bucket, prefix, token, limit))
	f.mu.Unlock()
	if f.blocked != nil && prefix == "folder/" {
		if f.entered != nil {
			close(f.entered)
		}
		<-f.blocked
	}
	if token == "" {
		return cloud.ObjectPage{Objects: []cloud.Object{{Key: prefix + "first"}}, NextToken: "opaque+/=token"}, nil
	}
	return cloud.ObjectPage{Objects: []cloud.Object{{Key: prefix + "second"}}}, nil
}
func paginationApp(f *pagedOSSFixture) *App {
	a := New(Platform{Store: &memStore{}, Storage: func(cloud.Account) (cloud.ObjectStorage, error) { return f, nil }})
	a.profiles = []cloud.Account{{ID: "a", Name: "fixture", Region: "cn-hangzhou"}}
	a.accountID = "a"
	a.bucket = "bucket"
	a.prefix = "folder/"
	return a
}
func finishPagination(a *App) { a.workers.Wait(); a.drain() }

func TestOSSNextPreviousReplacePageAndClearSelection(t *testing.T) {
	f := &pagedOSSFixture{}
	a := paginationApp(f)
	defer a.Close()
	a.refreshOSS(false)
	finishPagination(a)
	a.selected = map[string]bool{"folder/first": true}
	a.refreshOSS(true)
	if len(a.selectedKeys()) != 0 {
		t.Fatal("navigation kept hidden selection")
	}
	// The previous page may still be visible while its replacement loads.
	a.selected["folder/first"] = true
	finishPagination(a)
	if len(a.selectedKeys()) != 0 {
		t.Fatal("page replacement kept a selection made while loading")
	}
	if len(a.objects) != 1 || a.objects[0].Key != "folder/second" || a.nextToken != "" {
		t.Fatalf("second page accumulated objects: %+v", a.objects)
	}
	a.refreshOSS(true)
	finishPagination(a)
	if len(f.requests) != 2 {
		t.Fatal("last page issued another next-page request")
	}
	a.previousOSSPage()
	finishPagination(a)
	if len(a.objects) != 1 || a.objects[0].Key != "folder/first" {
		t.Fatalf("previous page: %+v", a.objects)
	}
	want := []string{"bucket|folder/||20", "bucket|folder/|opaque+/=token|20", "bucket|folder/||20"}
	if fmt.Sprint(f.requests) != fmt.Sprint(want) {
		t.Fatalf("requests %v", f.requests)
	}
}

func TestOSSSearchResetsAndDiscardsOldResponse(t *testing.T) {
	f := &pagedOSSFixture{blocked: make(chan struct{}), entered: make(chan struct{})}
	a := paginationApp(f)
	defer a.Close()
	a.refreshOSS(false)
	<-f.entered
	a.search = "report"
	a.ossSearchChanged()
	close(f.blocked)
	finishPagination(a)
	if len(a.objects) != 1 || a.objects[0].Key != "folder/reportfirst" {
		t.Fatalf("search page: %+v", a.objects)
	}
	if a.objectPager.page != 0 {
		t.Fatal("search kept previous page")
	}
}

func TestOSSUnsubmittedSearchDoesNotChangeNextPage(t *testing.T) {
	f := &pagedOSSFixture{}
	a := paginationApp(f)
	defer a.Close()
	a.refreshOSS(false)
	finishPagination(a)
	a.search = "not-submitted"
	a.refreshOSS(true)
	finishPagination(a)
	if len(a.objects) != 1 || a.objects[0].Key != "folder/second" {
		t.Fatalf("editing query changed active listing: %+v", a.objects)
	}
}

func TestOSSPageSizeAndDirectoryChangesRestartAtFirstPage(t *testing.T) {
	f := &pagedOSSFixture{}
	a := paginationApp(f)
	defer a.Close()
	a.refreshOSS(false)
	finishPagination(a)
	a.refreshOSS(true)
	finishPagination(a)
	a.objectPager.size = 50
	a.refreshOSS(false)
	finishPagination(a)
	if a.objectPager.page != 0 || f.requests[2] != "bucket|folder/||50" {
		t.Fatalf("size change did not restart: %v", f.requests)
	}
	a.search = "report"
	a.ossSearchChanged()
	finishPagination(a)
	a.prefix = "other/"
	a.refreshOSS(false)
	finishPagination(a)
	if a.objectPager.page != 0 || a.objectPager.query != "" || a.search != "" || a.objects[0].Key != "other/first" {
		t.Fatalf("directory retained prior query or page: %+v %+v", a.objectPager, a.objects)
	}
}

func TestOSSAccountSwitchDiscardsInFlightPage(t *testing.T) {
	f := &pagedOSSFixture{blocked: make(chan struct{}), entered: make(chan struct{})}
	a := paginationApp(f)
	defer a.Close()
	a.refreshOSS(false)
	<-f.entered
	a.switchAccount("another")
	close(f.blocked)
	finishPagination(a)
	if len(a.objects) != 0 || a.bucket != "" || len(a.selectedKeys()) != 0 {
		t.Fatal("previous account's pending page leaked into current account")
	}
}

func TestOSSTokenHistoryIsBoundedAndFirstPageRemainsReachable(t *testing.T) {
	p := objectPagination{}
	p.reset("scope")
	for page := 0; page < 1000; page++ {
		p.commit(page, fmt.Sprintf("token-%d", page), fmt.Sprintf("token-%d", page+1))
	}
	if len(p.tokens) > 256 {
		t.Fatalf("unbounded token history: %d", len(p.tokens))
	}
	if token, ok := p.token(998); !ok || token != "token-998" {
		t.Fatalf("previous token %q %v", token, ok)
	}
	if token, ok := p.token(0); !ok || token != "" {
		t.Fatalf("first page unavailable: %q %v", token, ok)
	}
}
