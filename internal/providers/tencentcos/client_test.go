package tencentcos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"yuntools/internal/cloud"
)

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(cloud.Account{
		ID:              "tc-1",
		Provider:        cloud.ProviderTencent,
		Region:          "ap-guangzhou",
		AccessKeyID:     "AKIDTEST",
		AccessKeySecret: "SECRETKEYTEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	c.serviceURL = u
	c.bucketURL = func(bucket string) (*url.URL, error) {
		return u, nil
	}
	return c
}

func TestListBucketsAndObjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Path == "/" && r.URL.Query().Get("prefix") == "" && r.URL.Query().Get("delimiter") == "" {
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
  <Buckets>
    <Bucket>
      <Name>demo-1250000000</Name>
      <Location>ap-guangzhou</Location>
      <CreationDate>2026-11-06T08:00:00Z</CreationDate>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult>
  <IsTruncated>true</IsTruncated>
  <NextMarker>next-1</NextMarker>
  <Contents>
    <Key>hello.txt</Key>
    <LastModified>2026-11-06T08:00:00Z</LastModified>
    <ETag>"abc"</ETag>
    <Size>12</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <CommonPrefixes>
    <Prefix>docs/</Prefix>
  </CommonPrefixes>
</ListBucketResult>`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	buckets, err := c.ListBuckets(context.Background())
	if err != nil || len(buckets) != 1 || buckets[0].Name != "demo-1250000000" || buckets[0].Region != "ap-guangzhou" {
		t.Fatalf("unexpected buckets: %+v err=%v", buckets, err)
	}
	page, err := c.ListObjectsPage(context.Background(), "demo-1250000000", "", "", 50)
	if err != nil || len(page.Objects) != 2 || page.NextToken != "next-1" {
		t.Fatalf("unexpected page: %+v err=%v", page, err)
	}
}

func TestObjectLinkSignedWhenACLPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	link, err := c.ObjectLink(context.Background(), "demo-1250000000", "secret/file.txt", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !link.Signed || !strings.Contains(link.URL, "secret/file.txt") || link.Reason == "" {
		t.Fatalf("unexpected link: %+v", link)
	}
}
