package qiniukodo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/qiniu/go-sdk/v7/storage"
	"yuntools/internal/cloud"
)

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
	domains, err := c.bm.ListBucketDomains(bucket)
	if err != nil {
		return cloud.ObjectLink{}, err
	}
	if len(domains) == 0 || strings.TrimSpace(domains[0].Domain) == "" {
		return cloud.ObjectLink{}, errors.New("该七牛云空间尚未绑定访问域名，请先在七牛云控制台绑定域名")
	}
	domainURL := strings.TrimSpace(domains[0].Domain)
	if !strings.HasPrefix(domainURL, "http://") && !strings.HasPrefix(domainURL, "https://") {
		domainURL = "https://" + domainURL
	}
	info, err := c.bm.GetBucketInfo(bucket)
	if err == nil && !info.IsPrivate() {
		return cloud.ObjectLink{
			URL: storage.MakePublicURL(domainURL, key),
		}, nil
	}
	reason := ""
	if err != nil {
		reason = "无法确认空间的公开权限，已使用签名链接"
	}
	deadline := time.Now().Add(expiry)
	return cloud.ObjectLink{
		URL:       storage.MakePrivateURL(c.mac, domainURL, key, deadline.Unix()),
		Signed:    true,
		ExpiresAt: deadline,
		Reason:    reason,
	}, nil
}
