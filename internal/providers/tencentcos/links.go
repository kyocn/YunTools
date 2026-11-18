package tencentcos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"yuntools/internal/cloud"
)

const allUsersURI = "http://cam.qcloud.com/groups/global/AllUsers"

func hasPublicReadGrant(grants []cos.ACLGrant) bool {
	for _, g := range grants {
		if g.Grantee != nil && g.Grantee.URI == allUsersURI {
			perm := strings.ToUpper(g.Permission)
			if perm == "READ" || perm == "FULL_CONTROL" {
				return true
			}
		}
	}
	return false
}

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
	bc, err := c.bucketClient(bucket)
	if err != nil {
		return cloud.ObjectLink{}, err
	}
	public := false
	reason := ""
	objACL, _, err := bc.Object.GetACL(ctx, key)
	if err == nil && objACL != nil && hasPublicReadGrant(objACL.AccessControlList) {
		public = true
	} else if err == nil {
		bACL, _, bErr := bc.Bucket.GetACL(ctx)
		if bErr == nil && bACL != nil && hasPublicReadGrant(bACL.AccessControlList) {
			public = true
		} else if bErr != nil {
			reason = "无法确认对象的公开权限，已使用签名链接"
		}
	} else {
		reason = "无法确认对象的公开权限，已使用签名链接"
	}
	var opt interface{}
	if c.account.SecurityToken != "" {
		opt = &cos.PresignedURLOptions{
			Query: &url.Values{"x-cos-security-token": {c.account.SecurityToken}},
		}
	}
	u, err := bc.Object.GetPresignedURL(ctx, http.MethodGet, key, c.account.AccessKeyID, c.account.AccessKeySecret, expiry, opt)
	if err != nil {
		return cloud.ObjectLink{}, err
	}
	if public {
		u.RawQuery = ""
		u.ForceQuery = false
		return cloud.ObjectLink{URL: u.String()}, nil
	}
	return cloud.ObjectLink{
		URL:       u.String(),
		Signed:    true,
		ExpiresAt: time.Now().Add(expiry),
		Reason:    reason,
	}, nil
}
