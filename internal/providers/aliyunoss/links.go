package aliyunoss

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"yuntools/internal/cloud"
)

var _ cloud.ObjectLinker = (*Client)(nil)

// ObjectLink checks the object ACL first; only the default ACL inherits the
// bucket ACL. Public ACLs are unsigned only when both block settings are known
// to be disabled. This performs read-only requests and never changes access.
func (c *Client) ObjectLink(ctx context.Context, bucket, key string, expiry time.Duration) (cloud.ObjectLink, error) {
	expiry, err := cloud.ObjectLinkExpiry(expiry)
	if err != nil {
		return cloud.ObjectLink{}, err
	}
	if bucket == "" || key == "" {
		return cloud.ObjectLink{}, errors.New("请选择要复制链接的对象")
	}
	if err := ctx.Err(); err != nil {
		return cloud.ObjectLink{}, err
	}
	// ACL discovery only decides whether a signature can be omitted. Failed or
	// unrecognized discovery must not prevent local signing with the credentials.
	acl := ""
	object, err := c.sdk.GetObjectAcl(ctx, &oss.GetObjectAclRequest{Bucket: &bucket, Key: &key})
	if err == nil && object != nil {
		acl = value(object.ACL)
	}
	if acl == "default" {
		acl = ""
		b, err := c.sdk.GetBucketAcl(ctx, &oss.GetBucketAclRequest{Bucket: &bucket})
		if err == nil && b != nil {
			acl = value(b.ACL)
		}
	}
	public := false
	reason := ""
	switch acl {
	case "public-read", "public-read-write":
		public = true
	case "private":
	default:
		reason = "无法确认对象的公开权限，已使用签名链接"
	}
	if public {
		public, reason = c.publicAccessAllowed(ctx, bucket)
	}
	if err := ctx.Err(); err != nil {
		return cloud.ObjectLink{}, err
	}
	// Let the SDK resolve the HTTPS endpoint and escape the exact key. It does
	// not issue an object GET. Public results discard the entire auth query.
	result, err := c.sdk.Presign(ctx, &oss.GetObjectRequest{Bucket: &bucket, Key: &key}, oss.PresignExpires(expiry))
	if err != nil {
		return cloud.ObjectLink{}, err
	}
	if public {
		u, err := url.Parse(result.URL)
		if err != nil {
			return cloud.ObjectLink{}, errors.New("无法生成对象链接")
		}
		u.RawQuery = ""
		u.ForceQuery = false
		return cloud.ObjectLink{URL: u.String()}, nil
	}
	return cloud.ObjectLink{URL: result.URL, Signed: true, ExpiresAt: result.Expiration, Reason: reason}, nil
}

func (c *Client) publicAccessAllowed(ctx context.Context, bucket string) (bool, string) {
	account, err := c.sdk.GetPublicAccessBlock(ctx, &oss.GetPublicAccessBlockRequest{})
	if err != nil || account.PublicAccessBlockConfiguration == nil || account.PublicAccessBlockConfiguration.BlockPublicAccess == nil {
		return false, "无法确认账号的公共访问限制，已使用签名链接"
	}
	if *account.PublicAccessBlockConfiguration.BlockPublicAccess {
		return false, "账号已阻止公共访问，已使用签名链接"
	}
	b, err := c.sdk.GetBucketPublicAccessBlock(ctx, &oss.GetBucketPublicAccessBlockRequest{Bucket: &bucket})
	if err != nil || b.PublicAccessBlockConfiguration == nil || b.PublicAccessBlockConfiguration.BlockPublicAccess == nil {
		return false, "无法确认 Bucket 的公共访问限制，已使用签名链接"
	}
	if *b.PublicAccessBlockConfiguration.BlockPublicAccess {
		return false, "Bucket 已阻止公共访问，已使用签名链接"
	}
	return true, ""
}
