// Package aliyunoss implements OSS using Alibaba's official v2 SDK. Clients are
// bound to an immutable account and regional endpoint, including an STS token.
package aliyunoss

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"yuntools/internal/cloud"
)

type Client struct {
	sdk               *oss.Client
	accountID, region string
}

var _ cloud.ObjectStorage = (*Client)(nil)

func New(a cloud.Account) (*Client, error) { return newClient(a, nil) }
func newClient(a cloud.Account, configure func(*oss.Config)) (*Client, error) {
	if a.ID == "" || a.Region == "" || a.AccessKeyID == "" || a.AccessKeySecret == "" {
		return nil, errors.New("OSS requires account, region and credentials")
	}
	cfg := oss.LoadDefaultConfig().WithRegion(a.Region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(a.AccessKeyID, a.AccessKeySecret, a.SecurityToken)).WithLogLevel(oss.LogOff)
	if configure != nil {
		configure(cfg)
	}
	return &Client{sdk: oss.NewClient(cfg), accountID: a.ID, region: a.Region}, nil
}
func value[T any](p *T) (z T) {
	if p != nil {
		return *p
	}
	return z
}
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func (c *Client) ListBuckets(ctx context.Context) ([]cloud.Bucket, error) {
	var buckets []cloud.Bucket
	marker := ""
	seen := map[string]bool{}
	for {
		r, err := c.sdk.ListBuckets(ctx, &oss.ListBucketsRequest{Marker: optional(marker), MaxKeys: 1000})
		if err != nil {
			return nil, err
		}
		for _, b := range r.Buckets {
			region := value(b.Region)
			if region == "" {
				region = strings.TrimPrefix(value(b.Location), "oss-")
			}
			buckets = append(buckets, cloud.Bucket{Name: value(b.Name), Region: region, StorageClass: value(b.StorageClass), Created: value(b.CreationDate)})
		}
		if !r.IsTruncated {
			return buckets, nil
		}
		marker = value(r.NextMarker)
		if marker == "" || seen[marker] {
			return nil, errors.New("OSS returned invalid bucket pagination")
		}
		seen[marker] = true
	}
}

// CreateBucket uses this client's regional endpoint. The third argument is region.
func (c *Client) CreateBucket(ctx context.Context, name, region string) error {
	if region != "" && region != c.region {
		return errors.New("bucket region differs from client region")
	}
	_, err := c.sdk.PutBucket(ctx, &oss.PutBucketRequest{Bucket: &name, Acl: oss.BucketACLPrivate})
	return err
}
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	_, err := c.sdk.DeleteBucket(ctx, &oss.DeleteBucketRequest{Bucket: &name})
	return err
}
func (c *Client) ListObjects(ctx context.Context, bucket, prefix, token string) (cloud.ObjectPage, error) {
	return c.ListObjectsPage(ctx, bucket, prefix, token, 1000)
}

func (c *Client) ListObjectsPage(ctx context.Context, bucket, prefix, token string, limit int) (cloud.ObjectPage, error) {
	if limit < 1 || limit > 1000 {
		return cloud.ObjectPage{}, errors.New("OSS page size must be between 1 and 1000")
	}
	r, err := c.sdk.ListObjectsV2(ctx, &oss.ListObjectsV2Request{Bucket: &bucket, Prefix: optional(prefix), ContinuationToken: optional(token), Delimiter: oss.Ptr("/"), MaxKeys: int32(limit)})
	if err != nil {
		return cloud.ObjectPage{}, err
	}
	p := cloud.ObjectPage{}
	for _, o := range r.Contents {
		p.Objects = append(p.Objects, cloud.Object{Key: value(o.Key), ETag: value(o.ETag), Size: o.Size, Modified: value(o.LastModified), StorageClass: value(o.StorageClass), IsDir: strings.HasSuffix(value(o.Key), "/")})
	}
	for _, d := range r.CommonPrefixes {
		p.Objects = append(p.Objects, cloud.Object{Key: value(d.Prefix), IsDir: true})
	}
	if r.IsTruncated {
		p.NextToken = value(r.NextContinuationToken)
		if p.NextToken == "" || p.NextToken == token {
			return cloud.ObjectPage{}, errors.New("OSS returned invalid object pagination")
		}
	}
	return p, nil
}
func object(key string, r *oss.HeadObjectResult) cloud.Object {
	return cloud.Object{Key: key, ETag: value(r.ETag), VersionID: value(r.VersionId), CRC64: value(r.HashCRC64), Size: r.ContentLength, Modified: value(r.LastModified), ContentType: value(r.ContentType), StorageClass: value(r.StorageClass), Metadata: maps.Clone(r.Metadata)}
}
func (c *Client) Stat(ctx context.Context, bucket, key string) (cloud.Object, error) {
	r, err := c.sdk.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: &bucket, Key: &key})
	if err != nil {
		return cloud.Object{}, err
	}
	return object(key, r), nil
}
func (c *Client) copyRequest(ctx context.Context, bucket, source, target string) (*oss.CopyObjectRequest, error) {
	r, err := c.sdk.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: &bucket, Key: &source})
	if err != nil {
		return nil, err
	}
	if value(r.ETag) == "" {
		return nil, errors.New("OSS source has no identity")
	}
	return &oss.CopyObjectRequest{Bucket: &bucket, Key: &target, SourceBucket: &bucket, SourceKey: &source, SourceVersionId: r.VersionId, IfMatch: r.ETag,
		ContentType: r.ContentType, ContentEncoding: r.ContentEncoding, ContentDisposition: r.ContentDisposition, CacheControl: r.CacheControl, Expires: r.Expires,
		StorageClass: oss.StorageClassType(value(r.StorageClass)), ServerSideEncryption: r.ServerSideEncryption, ServerSideEncryptionKeyId: r.ServerSideEncryptionKeyId, ServerSideDataEncryption: r.ServerSideDataEncryption}, nil
}
func (c *Client) SetMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	for k, v := range metadata {
		if k == "" || strings.ContainsAny(k, "\r\n:") || strings.ContainsAny(v, "\r\n") {
			return errors.New("invalid metadata")
		}
	}
	req, err := c.copyRequest(ctx, bucket, key, key)
	if err != nil {
		return err
	}
	req.MetadataDirective = oss.Ptr("REPLACE")
	req.Metadata = maps.Clone(metadata)
	return c.copyPinned(ctx, req)
}

// CopyObject never deletes its source. Moving requires a separately confirmed
// manifest after the destination has been verified by the caller.
func (c *Client) CopyObject(ctx context.Context, bucket, source, target string) error {
	if source == target {
		return errors.New("source and destination must differ")
	}
	req, err := c.copyRequest(ctx, bucket, source, target)
	if err != nil {
		return err
	}
	req.MetadataDirective = oss.Ptr("COPY")
	req.ForbidOverwrite = oss.Ptr("true")
	return c.copyPinned(ctx, req)
}

// DeleteObjects acts only on these exact current keys. Historical versions are
// never enumerated or deleted. Callers must obtain an explicit confirmation.
func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return errors.New("empty deletion manifest")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" || seen[key] {
			return errors.New("invalid deletion manifest")
		}
		seen[key] = true
	}
	for start := 0; start < len(keys); start += 1000 {
		end := min(start+1000, len(keys))
		entries := make([]oss.ObjectIdentifier, 0, end-start)
		for _, key := range keys[start:end] {
			entries = append(entries, oss.ObjectIdentifier{Key: oss.Ptr(key)})
		}
		r, err := c.sdk.DeleteMultipleObjects(ctx, &oss.DeleteMultipleObjectsRequest{Bucket: &bucket, Delete: &oss.Delete{Objects: entries, Quiet: false}})
		if err != nil {
			return err
		}
		deleted := map[string]bool{}
		for _, d := range r.DeletedObjects {
			deleted[value(d.Key)] = true
		}
		for _, key := range keys[start:end] {
			if !deleted[key] {
				return fmt.Errorf("OSS did not confirm deletion of %q; some objects may already have been deleted", key)
			}
		}
	}
	return nil
}

// CreateDirectory creates the conventional zero-byte object with a trailing slash.
func (c *Client) CreateDirectory(ctx context.Context, bucket, key string) error {
	if key == "" {
		return errors.New("directory name is required")
	}
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	_, err := c.sdk.PutObject(ctx, &oss.PutObjectRequest{Bucket: &bucket, Key: &key, Body: strings.NewReader(""), ContentType: oss.Ptr("application/x-directory"), ForbidOverwrite: oss.Ptr("true")})
	return err
}
