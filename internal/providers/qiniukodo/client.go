// Package qiniukodo implements Qiniu Cloud Kodo object storage using qiniu/go-sdk/v7.
package qiniukodo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qiniu/go-sdk/v7/auth"
	"github.com/qiniu/go-sdk/v7/storage"
	"yuntools/internal/cloud"
)

type Client struct {
	account cloud.Account
	mac     *auth.Credentials
	cfg     *storage.Config
	bm      *storage.BucketManager
}

var (
	_ cloud.ObjectStorage    = (*Client)(nil)
	_ cloud.ObjectPageLister = (*Client)(nil)
	_ cloud.ObjectLinker     = (*Client)(nil)
)

func New(a cloud.Account) (*Client, error) {
	if a.ID == "" || a.AccessKeyID == "" || a.AccessKeySecret == "" {
		return nil, errors.New("Qiniu Kodo requires account and credentials")
	}
	mac := auth.New(a.AccessKeyID, a.AccessKeySecret)
	cfg := &storage.Config{UseHTTPS: true, UseCdnDomains: false}
	if r := regionFromID(a.Region); r != nil {
		cfg.Region = r
	}
	bm := storage.NewBucketManager(mac, cfg)
	return &Client{
		account: a,
		mac:     mac,
		cfg:     cfg,
		bm:      bm,
	}, nil
}

func regionFromID(id string) *storage.Region {
	switch strings.TrimSpace(id) {
	case "z0", "cn-east-1":
		return &storage.ZoneHuadong
	case "cn-east-2":
		return &storage.ZoneHuadongZheJiang2
	case "z1", "cn-north-1":
		return &storage.ZoneHuabei
	case "z2", "cn-south-1":
		return &storage.ZoneHuanan
	case "na0", "us-north-1":
		return &storage.ZoneBeimei
	case "as0", "ap-southeast-1":
		return &storage.ZoneXinjiapo
	case "ap-northeast-1":
		if r, ok := storage.GetRegionByID(storage.RegionID("ap-northeast-1")); ok {
			return &r
		}
	}
	if id != "" {
		if r, ok := storage.GetRegionByID(storage.RegionID(id)); ok {
			return &r
		}
	}
	return nil
}

func storageClassLabel(t int) string {
	switch t {
	case 0:
		return "STANDARD"
	case 1:
		return "IA"
	case 2:
		return "ARCHIVE"
	case 3:
		return "DEEP_ARCHIVE"
	case 4:
		return "ARCHIVE_IR"
	default:
		return fmt.Sprintf("TYPE_%d", t)
	}
}

func parsePutTime(putTime int64) time.Time {
	if putTime <= 0 {
		return time.Time{}
	}
	return time.Unix(0, putTime*100)
}

func (c *Client) ListBuckets(ctx context.Context) ([]cloud.Bucket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := c.bm.Buckets(true)
	if err != nil {
		return nil, err
	}
	out := make([]cloud.Bucket, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := cloud.Bucket{Name: name}
		if info, err := c.bm.GetBucketInfo(name); err == nil {
			b.Region = info.Region
			if b.Region == "" {
				b.Region = info.Zone
			}
			b.Created = info.Ctime
		}
		out = append(out, b)
	}
	return out, nil
}

func (c *Client) CreateBucket(ctx context.Context, name, region string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if region == "" {
		region = c.account.Region
	}
	if region == "" {
		region = "z0"
	}
	return c.bm.CreateBucket(name, storage.RegionID(region))
}

func (c *Client) CreateDirectory(ctx context.Context, bucket, key string) error {
	key = strings.TrimLeft(key, "/")
	if key == "" {
		return errors.New("directory key is required")
	}
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	policy := storage.PutPolicy{Scope: fmt.Sprintf("%s:%s", bucket, key)}
	upToken := policy.UploadToken(c.mac)
	uploader := storage.NewFormUploader(c.cfg)
	var ret storage.PutRet
	return uploader.Put(ctx, &ret, upToken, key, strings.NewReader(""), 0, nil)
}

func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.bm.DropBucket(name)
}

func (c *Client) ListObjects(ctx context.Context, bucket, prefix, token string) (cloud.ObjectPage, error) {
	return c.ListObjectsPage(ctx, bucket, prefix, token, 1000)
}

func (c *Client) ListObjectsPage(ctx context.Context, bucket, prefix, token string, limit int) (cloud.ObjectPage, error) {
	if err := ctx.Err(); err != nil {
		return cloud.ObjectPage{}, err
	}
	if limit < 1 || limit > 1000 {
		return cloud.ObjectPage{}, errors.New("Kodo page size must be between 1 and 1000")
	}
	entries, commonPrefixes, nextMarker, hasNext, err := c.bm.ListFiles(bucket, prefix, "/", token, limit)
	if err != nil {
		return cloud.ObjectPage{}, err
	}
	page := cloud.ObjectPage{}
	for _, e := range entries {
		page.Objects = append(page.Objects, cloud.Object{
			Key:          e.Key,
			ETag:         e.Hash,
			Size:         e.Fsize,
			Modified:     parsePutTime(e.PutTime),
			ContentType:  e.MimeType,
			StorageClass: storageClassLabel(e.Type),
			IsDir:        strings.HasSuffix(e.Key, "/"),
		})
	}
	for _, dir := range commonPrefixes {
		page.Objects = append(page.Objects, cloud.Object{Key: dir, IsDir: true})
	}
	if hasNext && nextMarker != "" {
		if nextMarker == token {
			return cloud.ObjectPage{}, errors.New("Kodo returned invalid object pagination")
		}
		page.NextToken = nextMarker
	}
	return page, nil
}

func (c *Client) Stat(ctx context.Context, bucket, key string) (cloud.Object, error) {
	if err := ctx.Err(); err != nil {
		return cloud.Object{}, err
	}
	info, err := c.bm.Stat(bucket, key)
	if err != nil {
		return cloud.Object{}, err
	}
	return cloud.Object{
		Key:          key,
		ETag:         info.Hash,
		Size:         info.Fsize,
		Modified:     parsePutTime(info.PutTime),
		ContentType:  info.MimeType,
		StorageClass: storageClassLabel(info.Type),
		Metadata:     info.MetaData,
	}, nil
}

func (c *Client) SetMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.bm.ChangeMeta(bucket, key, metadata)
}

func (c *Client) CopyObject(ctx context.Context, bucket, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.bm.Copy(bucket, src, bucket, dst, true)
}

func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	for i := 0; i < len(keys); i += 1000 {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(i+1000, len(keys))
		ops := make([]string, 0, end-i)
		for _, k := range keys[i:end] {
			ops = append(ops, storage.URIDelete(bucket, k))
		}
		rets, err := c.bm.BatchWithContext(ctx, bucket, ops)
		if err != nil {
			return err
		}
		for idx, ret := range rets {
			if ret.Code != 200 {
				return fmt.Errorf("Kodo delete failed for %s: %s", keys[i+idx], ret.Data.Error)
			}
		}
	}
	return nil
}
