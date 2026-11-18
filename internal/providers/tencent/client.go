// Package tencent implements Tencent Cloud DNSPod and CDB/Redis/VPC whitelist capabilities.
package tencent

import (
	"errors"
	"strings"
	"time"

	cdb "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdb/v20170320"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
	redis "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/redis/v20180412"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	"yuntools/internal/cloud"
)

type Client struct {
	account cloud.Account
	cred    common.CredentialIface
}

var (
	_ cloud.DNS                    = (*Client)(nil)
	_ cloud.ChallengeDNS           = (*Client)(nil)
	_ cloud.Whitelist              = (*Client)(nil)
	_ cloud.DynamicWhitelist       = (*Client)(nil)
	_ cloud.SecurityGroupInspector = (*Client)(nil)
)

func New(a cloud.Account) (*Client, error) {
	if a.ID == "" || a.AccessKeyID == "" || a.AccessKeySecret == "" {
		return nil, errors.New("Tencent Cloud requires account and credentials")
	}
	var cred common.CredentialIface
	if strings.TrimSpace(a.SecurityToken) != "" {
		cred = common.NewTokenCredential(a.AccessKeyID, a.AccessKeySecret, a.SecurityToken)
	} else {
		cred = common.NewCredential(a.AccessKeyID, a.AccessKeySecret)
	}
	return &Client{account: a, cred: cred}, nil
}

func (c *Client) dnsClient() (*dnspod.Client, error) {
	return dnspod.NewClient(c.cred, "", profile.NewClientProfile())
}

func (c *Client) cdbClient(region string) (*cdb.Client, error) {
	return cdb.NewClient(c.cred, region, profile.NewClientProfile())
}

func (c *Client) redisClient(region string) (*redis.Client, error) {
	return redis.NewClient(c.cred, region, profile.NewClientProfile())
}

func (c *Client) vpcClient(region string) (*vpc.Client, error) {
	return vpc.NewClient(c.cred, region, profile.NewClientProfile())
}

var shanghaiLoc = time.FixedZone("CST", 8*3600)

func parseTencentTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, shanghaiLoc); err == nil {
		return t
	}
	return time.Time{}
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func u64Val(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

func i64Val(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
