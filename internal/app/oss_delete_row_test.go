package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

type rowDeleteOSS struct {
	cloud.ObjectStorage
	bucket string
	keys   []string
	pages  map[string]cloud.ObjectPage
}

func (s *rowDeleteOSS) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	s.bucket = bucket
	s.keys = append([]string(nil), keys...)
	return nil
}
func (s *rowDeleteOSS) ListObjects(_ context.Context, _ string, prefix string, _ string) (cloud.ObjectPage, error) {
	return s.pages[prefix], nil
}

func TestObjectRowDeleteConfirmsOnlyClickedFile(t *testing.T) {
	storage := &rowDeleteOSS{}
	a := New(Platform{Store: &memStore{}, Storage: func(cloud.Account) (cloud.ObjectStorage, error) { return storage, nil }})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a", Name: "测试账号", Region: "cn-shanghai"}}
	a.accountID, a.page, a.bucket = "a", "oss", "bucket"
	a.objects = []cloud.Object{{Key: "delete.txt"}, {Key: "keep.txt"}}
	a.selected = map[string]bool{"keep.txt": true}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("删除对象 delete.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.confirm == nil || !strings.Contains(a.confirm.Detail, "delete.txt") || strings.Contains(a.confirm.Detail, "keep.txt") {
		t.Fatal("confirmation does not match clicked file")
	}
	if len(storage.keys) != 0 {
		t.Fatal("deleted without second confirmation")
	}
	if err := tt.Click("取消操作"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(storage.keys) != 0 {
		t.Fatal("cancel deleted object")
	}
	if err := tt.Click("删除对象 delete.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	a.workers.Wait()
	if storage.bucket != "bucket" || !reflect.DeepEqual(storage.keys, []string{"delete.txt"}) {
		t.Fatal("deleted wrong scope", storage.bucket, storage.keys)
	}
}
func TestBucketListHasNoDeleteAction(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.accountID, a.page = "a", "oss"
	a.buckets = []cloud.Bucket{{Name: "keep-bucket", Region: "cn-shanghai"}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if _, exists := tt.Find("删除 Bucket keep-bucket"); exists {
		t.Fatal("bucket list still exposes delete")
	}
}

func TestFolderRowDeleteExpandsOnlyClickedFolder(t *testing.T) {
	storage := &rowDeleteOSS{pages: map[string]cloud.ObjectPage{"folder/": {Objects: []cloud.Object{{Key: "folder/child.txt"}}}}}
	a := New(Platform{Store: &memStore{}, Storage: func(cloud.Account) (cloud.ObjectStorage, error) { return storage, nil }})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a", Name: "测试账号", Region: "cn-shanghai"}}
	a.accountID, a.page, a.bucket = "a", "oss", "bucket"
	a.objects = []cloud.Object{{Key: "folder/", IsDir: true}, {Key: "keep.txt"}}
	a.selected = map[string]bool{"keep.txt": true}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("删除对象 folder/"); err != nil {
		t.Fatal(err)
	}
	a.workers.Wait()
	tt.Frame()
	if a.confirm == nil || !strings.Contains(a.confirm.Detail, "folder/child.txt") || strings.Contains(a.confirm.Detail, "keep.txt") {
		t.Fatal("folder confirmation has wrong manifest")
	}
	if len(storage.keys) != 0 {
		t.Fatal("folder deleted before confirmation")
	}
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	a.workers.Wait()
	if !reflect.DeepEqual(storage.keys, []string{"folder/", "folder/child.txt"}) {
		t.Fatal("folder deletion escaped its scope", storage.keys)
	}
}
