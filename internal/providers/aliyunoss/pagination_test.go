package aliyunoss

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestObjectPageUsesRequestedSizeAndOpaqueToken(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("max-keys") != "50" || r.URL.Query().Get("continuation-token") != "opaque+/=" || r.URL.Query().Get("prefix") != "folder/report" {
			t.Errorf("wrong page request %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>folder/report.csv</Key><Size>10</Size></Contents></ListBucketResult>`)
	})
	p, err := c.ListObjectsPage(context.Background(), "bucket", "folder/report", "opaque+/=", 50)
	if err != nil || len(p.Objects) != 1 || p.Objects[0].Key != "folder/report.csv" || p.NextToken != "" {
		t.Fatalf("page %+v %v", p, err)
	}
}
