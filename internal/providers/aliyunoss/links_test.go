package aliyunoss

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// These cases catch bucket ACL accidentally overriding an explicit object ACL.
func TestObjectLinkEffectiveACLAndEscaping(t *testing.T) {
	for _, tc := range []struct {
		name, objectACL, bucketACL string
		signed                     bool
		bucketCalls                int
	}{
		{"public object", "public-read", "private", false, 0},
		{"public writable object", "public-read-write", "private", false, 0},
		{"private override", "private", "public-read", true, 0},
		{"inherited public", "default", "public-read", false, 1},
		{"inherited public writable", "default", "public-read-write", false, 1},
		{"inherited private", "default", "private", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucketCalls := 0
			key := "folder/中文 space #?+%/file.txt"
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("publicAccessBlock") {
					fmt.Fprint(w, `<PublicAccessBlockConfiguration><BlockPublicAccess>false</BlockPublicAccess></PublicAccessBlockConfiguration>`)
					return
				}
				if r.Method != "GET" || !r.URL.Query().Has("acl") {
					t.Fatalf("unexpected request %s", r.Method)
				}
				acl := tc.objectACL
				if r.URL.Path == "/bucket/" {
					bucketCalls++
					acl = tc.bucketACL
				} else if r.URL.Path != "/bucket/"+key {
					t.Fatalf("wrong object path %q", r.URL.Path)
				}
				fmt.Fprintf(w, `<AccessControlPolicy><AccessControlList><Grant>%s</Grant></AccessControlList></AccessControlPolicy>`, acl)
			})
			before := time.Now()
			link, err := c.ObjectLink(context.Background(), "bucket", key, 0)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(link.URL)
			if err != nil {
				t.Fatal(err)
			}
			if u.Scheme != "https" || u.Path != "/bucket/"+key || u.Fragment != "" {
				t.Fatalf("bad escaped link path=%q fragment=%q", u.Path, u.Fragment)
			}
			if bucketCalls != tc.bucketCalls || link.Signed != tc.signed {
				t.Fatalf("ACL result signed=%v bucketCalls=%d", link.Signed, bucketCalls)
			}
			if tc.signed {
				q := u.Query()
				if q.Get("x-oss-signature") == "" || q.Get("x-oss-security-token") != "fixture-token" || q.Get("x-oss-expires") != "300" {
					t.Fatal("missing signature, STS token, or 300s default")
				}
				if link.ExpiresAt.Before(before.Add(299*time.Second)) || link.ExpiresAt.After(time.Now().Add(301*time.Second)) {
					t.Fatal("unexpected expiry")
				}
			} else if u.RawQuery != "" || !link.ExpiresAt.IsZero() {
				t.Fatal("public URL carries auth query or expiry")
			}
		})
	}
}

func TestObjectLinkRejectsInvalidExpiryBeforeRequests(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid expiry reached cloud") })
	for _, expiry := range []time.Duration{-time.Second, time.Millisecond, 604801 * time.Second} {
		if _, err := c.ObjectLink(context.Background(), "bucket", "key", expiry); err == nil {
			t.Fatalf("accepted expiry %v", expiry)
		}
	}
}
func TestObjectLinkCustomExpiry(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<AccessControlPolicy><AccessControlList><Grant>private</Grant></AccessControlList></AccessControlPolicy>`)
	})
	for _, seconds := range []int{1, 600, 604800} {
		link, err := c.ObjectLink(context.Background(), "bucket", "file", time.Duration(seconds)*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(link.URL)
		if u.Query().Get("x-oss-expires") != fmt.Sprint(seconds) {
			t.Fatal("custom expiry was ignored")
		}
	}
}

// ACL discovery is optional: failure must never block locally signed downloads.
func TestObjectLinkSignsWhenACLUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, objectACL, bucketACL string
		objectStatus, bucketStatus int
	}{
		{"object access denied", "", "", 403, 200},
		{"bucket access denied", "default", "", 200, 403},
		{"object unknown", "unexpected", "", 200, 200},
		{"object empty", "", "", 200, 200},
		{"bucket unknown", "default", "unexpected", 200, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !r.URL.Query().Has("acl") {
					t.Errorf("unexpected cloud request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				acl, status := tc.objectACL, tc.objectStatus
				if r.URL.Path == "/bucket/" {
					acl, status = tc.bucketACL, tc.bucketStatus
				}
				w.WriteHeader(status)
				if status == 403 {
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>You do not have read acl permission on this object.</Message></Error>`)
					return
				}
				fmt.Fprintf(w, `<AccessControlPolicy><AccessControlList><Grant>%s</Grant></AccessControlList></AccessControlPolicy>`, acl)
			})
			for _, expiry := range []time.Duration{0, 600 * time.Second} {
				link, err := c.ObjectLink(context.Background(), "bucket", "folder/中文 #?.txt", expiry)
				if err != nil {
					t.Fatal("ACL discovery blocked signing:", err)
				}
				u, err := url.Parse(link.URL)
				if err != nil {
					t.Fatal(err)
				}
				seconds := "300"
				if expiry != 0 {
					seconds = "600"
				}
				q := u.Query()
				if !link.Signed || link.Reason == "" || q.Get("x-oss-signature") == "" || q.Get("x-oss-security-token") != "fixture-token" || q.Get("x-oss-expires") != seconds || u.Path != "/bucket/folder/中文 #?.txt" {
					t.Fatal("missing signed fallback, expiry, credentials or exact object path")
				}
			}
		})
	}
}

func TestObjectLinkCancellationDoesNotProduceLink(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled operation issued request") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	link, err := c.ObjectLink(ctx, "bucket", "key", 0)
	if err == nil || link.URL != "" {
		t.Fatal("cancelled request produced a link")
	}
}

func TestObjectLinkPublicBlocksRequireSigning(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"account blocked", "/", "<BlockPublicAccess>true</BlockPublicAccess>", 200},
		{"bucket blocked", "/bucket/", "<BlockPublicAccess>true</BlockPublicAccess>", 200},
		{"account denied", "/", "", 403},
		{"bucket denied", "/bucket/", "", 403},
		{"unknown account", "/", "", 200},
		{"unknown bucket", "/bucket/", "", 200},
		{"bucket network error", "/bucket/", "", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("acl") {
					fmt.Fprint(w, `<AccessControlPolicy><AccessControlList><Grant>public-read</Grant></AccessControlList></AccessControlPolicy>`)
					return
				}
				body := "<BlockPublicAccess>false</BlockPublicAccess>"
				if r.URL.Path == tc.path {
					w.WriteHeader(tc.status)
					body = tc.body
				}
				fmt.Fprint(w, "<PublicAccessBlockConfiguration>"+body+"</PublicAccessBlockConfiguration>")
			})
			link, err := c.ObjectLink(context.Background(), "bucket", "file", 0)
			if err != nil {
				t.Fatal(err)
			}
			if !link.Signed || link.Reason == "" {
				t.Fatal("blocked/unknown public access did not generate explained signed fallback")
			}
		})
	}
}
