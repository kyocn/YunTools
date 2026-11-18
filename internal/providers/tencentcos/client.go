// Package tencentcos implements Tencent Cloud COS using the official cos-go-sdk-v5 SDK.
package tencentcos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"yuntools/internal/cloud"
)

type Client struct {
	account    cloud.Account
	httpClient *http.Client
	serviceURL *url.URL
	bucketURL  func(bucket string) (*url.URL, error)
}

var (
	_ cloud.ObjectStorage    = (*Client)(nil)
	_ cloud.ObjectPageLister = (*Client)(nil)
	_ cloud.ObjectLinker     = (*Client)(nil)
)

func New(a cloud.Account) (*Client, error) {
	if a.ID == "" || a.Region == "" || a.AccessKeyID == "" || a.AccessKeySecret == "" {
		return nil, errors.New("COS requires account, region and credentials")
	}
	serviceURL, err := url.Parse("https://service.cos.myqcloud.com")
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     a.AccessKeyID,
			SecretKey:    a.AccessKeySecret,
			SessionToken: a.SecurityToken,
		},
	}
	return &Client{
		account:    a,
		httpClient: httpClient,
		serviceURL: serviceURL,
		bucketURL: func(bucket string) (*url.URL, error) {
			return cos.NewBucketURL(bucket, a.Region, true)
		},
	}, nil
}

func (c *Client) serviceClient() *cos.Client {
	return cos.NewClient(&cos.BaseURL{ServiceURL: c.serviceURL}, c.httpClient)
}

func (c *Client) bucketClient(bucket string) (*cos.Client, error) {
	if bucket == "" {
		return nil, errors.New("bucket name is required")
	}
	bu, err := c.bucketURL(bucket)
	if err != nil {
		return nil, err
	}
	return cos.NewClient(&cos.BaseURL{BucketURL: bu, ServiceURL: c.serviceURL}, c.httpClient), nil
}

func parseCOSTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (c *Client) ListBuckets(ctx context.Context) ([]cloud.Bucket, error) {
	res, _, err := c.serviceClient().Service.Get(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]cloud.Bucket, 0, len(res.Buckets))
	for _, b := range res.Buckets {
		out = append(out, cloud.Bucket{
			Name:    b.Name,
			Region:  b.Region,
			Created: parseCOSTime(b.CreationDate),
		})
	}
	return out, nil
}

func (c *Client) CreateBucket(ctx context.Context, name, region string) error {
	if region != "" && region != c.account.Region {
		return errors.New("bucket region differs from client region")
	}
	bc, err := c.bucketClient(name)
	if err != nil {
		return err
	}
	_, err = bc.Bucket.Put(ctx, nil)
	return err
}

func (c *Client) CreateDirectory(ctx context.Context, bucket, key string) error {
	key = strings.TrimLeft(key, "/")
	if key == "" {
		return errors.New("directory key is required")
	}
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return err
	}
	_, err = bc.Object.Put(ctx, key, strings.NewReader(""), nil)
	return err
}

func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	bc, err := c.bucketClient(name)
	if err != nil {
		return err
	}
	_, err = bc.Bucket.Delete(ctx)
	return err
}

func (c *Client) ListObjects(ctx context.Context, bucket, prefix, token string) (cloud.ObjectPage, error) {
	return c.ListObjectsPage(ctx, bucket, prefix, token, 1000)
}

func (c *Client) ListObjectsPage(ctx context.Context, bucket, prefix, token string, limit int) (cloud.ObjectPage, error) {
	if limit < 1 || limit > 1000 {
		return cloud.ObjectPage{}, errors.New("COS page size must be between 1 and 1000")
	}
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return cloud.ObjectPage{}, err
	}
	res, _, err := bc.Bucket.Get(ctx, &cos.BucketGetOptions{
		Prefix:    prefix,
		Delimiter: "/",
		Marker:    token,
		MaxKeys:   limit,
	})
	if err != nil {
		return cloud.ObjectPage{}, err
	}
	page := cloud.ObjectPage{}
	for _, o := range res.Contents {
		page.Objects = append(page.Objects, cloud.Object{
			Key:          o.Key,
			ETag:         o.ETag,
			Size:         o.Size,
			Modified:     parseCOSTime(o.LastModified),
			StorageClass: o.StorageClass,
			IsDir:        strings.HasSuffix(o.Key, "/"),
		})
	}
	for _, dir := range res.CommonPrefixes {
		page.Objects = append(page.Objects, cloud.Object{Key: dir, IsDir: true})
	}
	if res.IsTruncated {
		page.NextToken = res.NextMarker
		if page.NextToken == "" && len(res.Contents) > 0 {
			page.NextToken = res.Contents[len(res.Contents)-1].Key
		}
		if page.NextToken == "" || page.NextToken == token {
			return cloud.ObjectPage{}, errors.New("COS returned invalid object pagination")
		}
	}
	return page, nil
}

func (c *Client) Stat(ctx context.Context, bucket, key string) (cloud.Object, error) {
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return cloud.Object{}, err
	}
	resp, err := bc.Object.Head(ctx, key, nil)
	if err != nil {
		return cloud.Object{}, err
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	meta := map[string]string{}
	for k, vals := range resp.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-cos-meta-") && len(vals) > 0 {
			meta[strings.TrimPrefix(lower, "x-cos-meta-")] = vals[0]
		}
	}
	return cloud.Object{
		Key:          key,
		ETag:         resp.Header.Get("ETag"),
		VersionID:    resp.Header.Get("x-cos-version-id"),
		StorageClass: resp.Header.Get("x-cos-storage-class"),
		ContentType:  resp.Header.Get("Content-Type"),
		CRC64:        resp.Header.Get("x-cos-hash-crc64ecma"),
		Size:         size,
		Modified:     parseCOSTime(resp.Header.Get("Last-Modified")),
		Metadata:     meta,
	}, nil
}

func (c *Client) copySourceURL(bucket, key string) (string, error) {
	bu, err := c.bucketURL(bucket)
	if err != nil {
		return "", err
	}
	escaped := strings.ReplaceAll(url.PathEscape(key), "%2F", "/")
	return fmt.Sprintf("%s/%s", bu.Host, strings.TrimLeft(escaped, "/")), nil
}

func (c *Client) SetMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return err
	}
	sourceURL, err := c.copySourceURL(bucket, key)
	if err != nil {
		return err
	}
	h := http.Header{}
	for k, v := range metadata {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(k), "x-cos-meta-") {
			k = "x-cos-meta-" + k
		}
		h.Set(k, v)
	}
	opt := &cos.ObjectCopyOptions{
		ObjectCopyHeaderOptions: &cos.ObjectCopyHeaderOptions{
			XCosMetadataDirective: "Replaced",
			XCosMetaXXX:           &h,
		},
	}
	_, _, err = bc.Object.Copy(ctx, key, sourceURL, opt)
	return err
}

func (c *Client) CopyObject(ctx context.Context, bucket, src, dst string) error {
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return err
	}
	sourceURL, err := c.copySourceURL(bucket, src)
	if err != nil {
		return err
	}
	_, _, err = bc.Object.Copy(ctx, dst, sourceURL, nil)
	return err
}

func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return err
	}
	for i := 0; i < len(keys); i += 1000 {
		end := min(i+1000, len(keys))
		objs := make([]cos.Object, 0, end-i)
		for _, k := range keys[i:end] {
			objs = append(objs, cos.Object{Key: k})
		}
		res, _, err := bc.Object.DeleteMulti(ctx, &cos.ObjectDeleteMultiOptions{
			Objects: objs,
			Quiet:   true,
		})
		if err != nil {
			return err
		}
		if res != nil && len(res.Errors) > 0 {
			return fmt.Errorf("COS delete failed for %s: %s", res.Errors[0].Key, res.Errors[0].Message)
		}
	}
	return nil
}
