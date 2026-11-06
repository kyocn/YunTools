package aliyunoss

import (
	"context"
	"fmt"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"yuntools/internal/cloud"
)

type fixtureTransport struct{ handler http.HandlerFunc }

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	f.handler(w, r)
	res := w.Result()
	res.Request = r
	return res, nil
}
func fixture(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	c, err := newClient(cloud.Account{ID: "acct", Region: "cn-hangzhou", AccessKeyID: "fixture-id", AccessKeySecret: "fixture-secret", SecurityToken: "fixture-token"}, func(cfg *oss.Config) {
		cfg.WithEndpoint("https://fixture.invalid").WithUsePathStyle(true).WithRetryMaxAttempts(1).WithHttpClient(&http.Client{Transport: fixtureTransport{h}})
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestListPaginationAndToken(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-oss-security-token") != "fixture-token" {
			t.Error("missing STS token")
		}
		if r.URL.Path == "/" {
			calls++
			if calls == 1 {
				fmt.Fprint(w, `<ListAllMyBucketsResult><IsTruncated>true</IsTruncated><NextMarker>next</NextMarker><Buckets><Bucket><Name>one</Name><Region>cn-hangzhou</Region></Bucket></Buckets></ListAllMyBucketsResult>`)
			} else {
				if r.URL.Query().Get("marker") != "next" {
					t.Error("missing marker")
				}
				fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>two</Name></Bucket></Buckets></ListAllMyBucketsResult>`)
			}
			return
		}
		if r.URL.Query().Get("continuation-token") != "token" || r.URL.Query().Get("prefix") != "dir/" || r.URL.Query().Get("delimiter") != "/" {
			t.Errorf("wrong query %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next-token</NextContinuationToken><Contents><Key>dir/file</Key><Size>8</Size><ETag>abc</ETag></Contents><CommonPrefixes><Prefix>dir/sub/</Prefix></CommonPrefixes></ListBucketResult>`)
	})
	bs, err := c.ListBuckets(context.Background())
	if err != nil || len(bs) != 2 {
		t.Fatalf("buckets %v %v", bs, err)
	}
	p, err := c.ListObjects(context.Background(), "bucket", "dir/", "token")
	if err != nil || len(p.Objects) != 2 || p.NextToken != "next-token" || !p.Objects[1].IsDir {
		t.Fatalf("page %+v %v", p, err)
	}
}
func TestMetadataPreservesHeadersAndCopyDoesNotDelete(t *testing.T) {
	puts := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			w.Header().Set("ETag", `"etag"`)
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Cache-Control", "max-age=42")
			w.Header().Set("x-oss-server-side-encryption", "KMS")
			w.Header().Set("x-oss-server-side-encryption-key-id", "key-id")
			w.Header().Set("Content-Length", "4")
		case "PUT":
			puts++
			if r.Header.Get("x-oss-copy-source-if-match") != `"etag"` {
				t.Error("copy not guarded by source identity")
			}
			if puts == 1 {
				if r.Header.Get("Content-Type") != "text/plain" || r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("Cache-Control") != "max-age=42" || r.Header.Get("x-oss-server-side-encryption") != "KMS" || r.Header.Get("x-oss-meta-owner") != "me" {
					t.Errorf("lost metadata: %v", r.Header)
				}
			}
			fmt.Fprint(w, `<CopyObjectResult><ETag>new</ETag></CopyObjectResult>`)
		default:
			t.Errorf("unexpected mutation %s", r.Method)
			w.WriteHeader(500)
		}
	})
	if err := c.SetMetadata(context.Background(), "bucket", "source", map[string]string{"owner": "me"}); err != nil {
		t.Fatal(err)
	}
	if err := c.CopyObject(context.Background(), "bucket", "source", "target"); err != nil {
		t.Fatal(err)
	}
	if puts != 2 {
		t.Fatalf("copies %d", puts)
	}
}
func TestDeleteOnlyExactCurrentKeysAndIncompleteResponseFails(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "VersionId") || !strings.Contains(string(body), "<Key>a</Key>") || !strings.Contains(string(body), "<Key>b</Key>") {
			t.Errorf("unexpected delete body %s", body)
		}
		fmt.Fprint(w, `<DeleteResult><Deleted><Key>a</Key></Deleted></DeleteResult>`)
	})
	if err := c.DeleteObjects(context.Background(), "bucket", []string{"a", "b"}); err == nil {
		t.Fatal("incomplete deletion claimed successful")
	}
}
func TestUploadProgressAndChangedSourceRejectedOnResume(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file")
	os.WriteFile(p, []byte("data"), 0600)
	cp := filepath.Join(dir, "cp")
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "data" {
			t.Errorf("body %q", body)
		}
		w.WriteHeader(503)
	})
	tr := cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: p, CheckpointDir: cp}
	var progress cloud.Progress
	if err := c.Upload(context.Background(), tr, func(p cloud.Progress) { progress = p }); err == nil {
		t.Fatal("expected upload failure")
	}
	if progress.Completed != 4 || progress.Total != 4 {
		t.Fatalf("progress %+v", progress)
	}
	os.WriteFile(p, []byte("evil"), 0600)
	before := calls
	if err := c.Upload(context.Background(), tr, nil); err == nil {
		t.Fatal("resumed changed source")
	}
	if calls != before {
		t.Fatal("source mismatch reached cloud")
	}
}
func TestDownloadPinsVersionAndETag(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("versionId") != "version-1" || r.Header.Get("If-Match") != `"etag"` {
			t.Errorf("missing identity %s %v", r.URL, r.Header)
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("x-oss-version-id", "version-1")
		w.Header().Set("Content-Length", "4")
		if r.Method == "GET" {
			fmt.Fprint(w, "data")
		}
	})
	dir := t.TempDir()
	dst := filepath.Join(dir, "output")
	var p cloud.Progress
	err := c.Download(context.Background(), cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: dst, DestinationRoot: dir, CheckpointDir: filepath.Join(dir, "cp"), ETag: `"etag"`, VersionID: "version-1", Size: 4}, func(x cloud.Progress) { p = x })
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "data" || p.Completed != 4 {
		t.Fatalf("download %q %+v %v", b, p, err)
	}
}

func TestMultipartPausePreservesCheckpointAndResumesUploadID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	data := strings.Repeat("a", 13*1024*1024)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var resumed atomic.Bool
	var initiations, aborts, partOne atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			initiations.Add(1)
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>key</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "GET" && q.Get("uploadId") == "upload-1":
			fmt.Fprintf(w, `<ListPartsResult><Bucket>bucket</Bucket><Key>key</Key><UploadId>upload-1</UploadId><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>part-one</ETag><Size>%d</Size></Part></ListPartsResult>`, 6*1024*1024)
		case r.Method == "PUT" && q.Get("partNumber") != "":
			if !resumed.Load() && q.Get("partNumber") != "1" {
				<-r.Context().Done()
				w.WriteHeader(499)
				fmt.Fprint(w, `<Error><Code>Cancelled</Code><Message>cancelled</Message></Error>`)
				return
			}
			if q.Get("partNumber") == "1" {
				partOne.Add(1)
			}
			io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", "part-one")
		case r.Method == "POST" && q.Get("uploadId") == "upload-1":
			fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>complete</ETag></CompleteMultipartUploadResult>`)
		case r.Method == "DELETE":
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	tr := cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: path, CheckpointDir: filepath.Join(dir, "cp")}
	if err := c.Upload(ctx, tr, func(p cloud.Progress) {
		if p.Completed >= 6*1024*1024 {
			cancel()
		}
	}); err == nil {
		t.Fatal("paused upload reported success")
	}
	if aborts.Load() != 0 {
		t.Fatal("pause aborted multipart upload")
	}
	cps, err := filepath.Glob(filepath.Join(tr.CheckpointDir, "*", "*"+oss.CheckpointFileSuffixUploader))
	if err != nil || len(cps) != 1 {
		t.Fatalf("missing SDK checkpoint: %v %v", cps, err)
	}
	resumed.Store(true)
	if err := c.Upload(context.Background(), tr, nil); err != nil {
		t.Fatal(err)
	}
	if initiations.Load() != 1 || partOne.Load() != 1 {
		t.Fatalf("did not resume existing parts: initiations=%d part-one=%d", initiations.Load(), partOne.Load())
	}
}

func TestDownloadChangedSourceAfterPauseDoesNotPublish(t *testing.T) {
	var changed atomic.Bool
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if changed.Load() {
			if r.Header.Get("If-Match") != `"old"` {
				t.Error("resume lost original ETag")
			}
			w.WriteHeader(412)
			fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>changed</Message></Error>`)
			return
		}
		w.Header().Set("ETag", `"old"`)
		w.Header().Set("Content-Length", "4")
		if r.Method == "GET" {
			w.WriteHeader(503)
		}
	})
	dir := t.TempDir()
	tr := cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: filepath.Join(dir, "out"), DestinationRoot: dir, CheckpointDir: filepath.Join(dir, "cp")}
	if err := c.Download(context.Background(), tr, nil); err == nil {
		t.Fatal("initial failure expected")
	}
	changed.Store(true)
	if err := c.Download(context.Background(), tr, nil); err == nil {
		t.Fatal("download resumed changed source")
	}
	if _, err := os.Stat(tr.LocalPath); !os.IsNotExist(err) {
		t.Fatalf("published invalid download: %v", err)
	}
}

func TestCreateDirectoryAndPrivateBucket(t *testing.T) {
	directories, buckets, deletes := 0, 0, 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/bucket/folder/":
			directories++
			b, _ := io.ReadAll(r.Body)
			if len(b) != 0 {
				t.Fatal("nonempty directory placeholder")
			}
		case r.Method == "PUT" && r.URL.Path == "/bucket/":
			buckets++
			if r.Header.Get("x-oss-acl") != "private" {
				t.Fatal("bucket not private")
			}
		case r.Method == "DELETE" && r.URL.Path == "/bucket/":
			deletes++
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		w.WriteHeader(200)
	})
	if err := c.CreateDirectory(context.Background(), "bucket", "folder"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateDirectory(context.Background(), "bucket", ""); err == nil {
		t.Fatal("accepted empty folder")
	}
	if err := c.CreateBucket(context.Background(), "bucket", "cn-hangzhou"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	if directories != 1 || buckets != 1 || deletes != 1 {
		t.Fatalf("wrong operations %d %d %d", directories, buckets, deletes)
	}
}

func TestPublishAnchorsToSelectedRootAfterSymlinkSwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "folder", "file")
	if err := os.Symlink(outside, filepath.Join(root, "folder")); err != nil {
		t.Fatal(err)
	}
	if err := publishDownloadRoot(context.Background(), source, destination, root); err == nil {
		t.Fatal("published through swapped symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatal("download escaped root")
	}
	if err := publishDownloadRoot(context.Background(), source, filepath.Join(outside, "file"), root); err == nil {
		t.Fatal("accepted outside destination")
	}
}

func TestCheckpointDoesNotCrossAccounts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file")
	if err := os.WriteFile(p, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(503)
	})
	tr := cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: p, CheckpointDir: filepath.Join(dir, "cp")}
	if err := c.Upload(context.Background(), tr, nil); err == nil {
		t.Fatal("fixture must fail")
	}
	if err := os.WriteFile(p, []byte("next"), 0600); err != nil {
		t.Fatal(err)
	}
	other := *c
	other.accountID = "other-account"
	if err := other.Upload(context.Background(), tr, nil); err == nil {
		t.Fatal("fixture must fail")
	}
	if calls.Load() != 2 {
		t.Fatal("second account reused first account's checkpoint identity")
	}
	if err := c.Upload(context.Background(), tr, nil); err == nil {
		t.Fatal("original account lost its pinned source")
	}
	if calls.Load() != 2 {
		t.Fatal("changed source reached cloud")
	}
}

func TestDownloadRequiresSelectedRootBeforeCloudAccess(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(404) })
	dir := t.TempDir()
	if err := c.Download(context.Background(), cloud.Transfer{Bucket: "bucket", Key: "key", LocalPath: filepath.Join(dir, "out"), CheckpointDir: filepath.Join(dir, "cp")}, nil); err == nil {
		t.Fatal("missing destination root accepted")
	}
	if calls != 0 {
		t.Fatal("download contacted cloud without an anchored destination")
	}
}

func publishDownloadRoot(ctx context.Context, source, destination, selectedRoot string) error {
	root, relative, err := openDownloadRoot(destination, selectedRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	return publishDownloadInto(ctx, source, root, relative)
}

// Regression: SDK v1.6.1 Copier drops IfMatch when constructing multipart
// UploadPartCopy requests. A source change must fail, never publish mixed parts.
func TestLargeCopySourceChangeNeverCompletes(t *testing.T) {
	for _, operation := range []string{"copy", "metadata"} {
		t.Run(operation, func(t *testing.T) {
			var parts, completed, aborted, unguarded atomic.Int32
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch {
				case r.Method == "HEAD":
					w.Header().Set("Content-Length", "268435456")
					w.Header().Set("ETag", `"original"`)
					w.Header().Set("x-oss-storage-class", "Standard")
					w.Header().Set("Content-Type", "text/plain")
				case r.Method == "POST" && q.Has("uploads"):
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>copy-1</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == "PUT" && q.Get("partNumber") != "":
					n := parts.Add(1)
					if r.Header.Get("x-oss-copy-source-if-match") != `"original"` {
						unguarded.Add(1)
					} else if n > 1 {
						w.WriteHeader(412)
						fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>source changed</Message></Error>`)
						return
					}
					fmt.Fprint(w, `<CopyPartResult><ETag>part</ETag></CopyPartResult>`)
				case r.Method == "POST" && q.Get("uploadId") == "copy-1":
					completed.Add(1)
					fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>mixed</ETag></CompleteMultipartUploadResult>`)
				case r.Method == "DELETE" && q.Get("uploadId") == "copy-1":
					aborted.Add(1)
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected operation %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			})
			var err error
			if operation == "copy" {
				err = c.CopyObject(context.Background(), "bucket", "source", "target")
			} else {
				err = c.SetMetadata(context.Background(), "bucket", "source", map[string]string{"owner": "me"})
			}
			if err == nil {
				t.Error("changed source copy reported success")
			}
			if unguarded.Load() != 0 {
				t.Errorf("%d multipart requests omitted the pinned ETag", unguarded.Load())
			}
			if completed.Load() != 0 {
				t.Fatal("completed a mixed-source multipart copy")
			}
			if aborted.Load() != 1 {
				t.Errorf("incomplete copy not aborted: %d", aborted.Load())
			}
		})
	}
}

func TestLargeCopyPreservesSourceIdentityOnEveryPart(t *testing.T) {
	var parts, completed atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == "HEAD":
			w.Header().Set("Content-Length", "268435456")
			w.Header().Set("ETag", `"original"`)
			w.Header().Set("x-oss-version-id", "v1")
			w.Header().Set("x-oss-storage-class", "Standard")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("x-oss-meta-owner", "me")
		case r.Method == "POST" && q.Has("uploads"):
			if r.Header.Get("Content-Type") != "text/plain" || r.Header.Get("x-oss-meta-owner") != "me" {
				t.Error("source metadata lost")
			}
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>copy-1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT" && q.Get("partNumber") != "":
			parts.Add(1)
			if r.Header.Get("x-oss-copy-source-if-match") != `"original"` || !strings.Contains(r.Header.Get("x-oss-copy-source"), "versionId=v1") {
				t.Errorf("part lost source identity: condition=%q source=%q", r.Header.Get("x-oss-copy-source-if-match"), r.Header.Get("x-oss-copy-source"))
			}
			fmt.Fprint(w, `<CopyPartResult><ETag>part</ETag></CopyPartResult>`)
		case r.Method == "POST" && q.Get("uploadId") == "copy-1":
			completed.Add(1)
			if r.Header.Get("x-oss-forbid-overwrite") != "true" {
				t.Error("completion lost destination overwrite guard")
			}
			fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>complete</ETag></CompleteMultipartUploadResult>`)
		default:
			t.Errorf("unexpected operation %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	if err := c.CopyObject(context.Background(), "bucket", "source", "target"); err != nil {
		t.Fatal(err)
	}
	if parts.Load() != 4 || completed.Load() != 1 {
		t.Fatalf("copy incomplete: parts=%d complete=%d", parts.Load(), completed.Load())
	}
}

func TestStatExposesCRC64ForMultipartCopyVerification(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Length", "268435456")
		w.Header().Set("ETag", `"multipart-etag-4"`)
		w.Header().Set("x-oss-hash-crc64ecma", "18446744073709551615")
	})
	obj, err := c.Stat(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	if obj.CRC64 != "18446744073709551615" {
		t.Fatalf("CRC64 not preserved losslessly: %q", obj.CRC64)
	}
	if obj.ETag != `"multipart-etag-4"` || obj.Size != 268435456 {
		t.Fatalf("stat identity lost: %+v", obj)
	}
}
