// Package aliyun implements additive DNS and whitelist operations using Alibaba's signed RPC SDK.
package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"yuntools/internal/cloud"
)

type rpcFunc func(context.Context, string, string, map[string]string, any) error
type Client struct {
	account   cloud.Account
	call      rpcFunc
	transport http.RoundTripper
}

var _ cloud.ChallengeDNS = (*Client)(nil)
var _ cloud.Whitelist = (*Client)(nil)
var regionPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+$`)

func New(a cloud.Account) (*Client, error) {
	if a.ID == "" || a.AccessKeyID == "" || a.AccessKeySecret == "" {
		return nil, errors.New("account credentials are required")
	}
	if a.Region == "" {
		a.Region = "cn-hangzhou"
	}
	if !regionPattern.MatchString(a.Region) {
		return nil, errors.New("invalid region")
	}
	c := &Client{account: a}
	c.call = c.rpc
	return c, nil
}

type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r.Clone(t.ctx))
}
func (c *Client) rpc(ctx context.Context, product, action string, params map[string]string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cred auth.Credential = credentials.NewAccessKeyCredential(c.account.AccessKeyID, c.account.AccessKeySecret)
	if c.account.SecurityToken != "" {
		cred = credentials.NewStsTokenCredential(c.account.AccessKeyID, c.account.AccessKeySecret, c.account.SecurityToken)
	}
	cfg := sdk.NewConfig().WithAutoRetry(false).WithTimeout(30 * time.Second)
	cfg.Scheme = "HTTPS"
	transport := c.transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cfg.Transport = contextTransport{ctx, transport}
	client, err := sdk.NewClientWithOptions(c.account.Region, cfg, cred)
	if err != nil {
		return errors.New("cannot initialize cloud client")
	}
	req := requests.NewCommonRequest()
	req.Method = "POST"
	req.Scheme = "https"
	req.ApiName = action
	region := params["RegionId"]
	if region == "" {
		region = c.account.Region
	}
	if !regionPattern.MatchString(region) {
		return errors.New("invalid region")
	}
	switch product {
	case "dns":
		req.Product = "Alidns"
		req.Version = "2015-01-09"
		req.Domain = "alidns.aliyuncs.com"
	case "rds":
		req.Product = "Rds"
		req.Version = "2014-08-15"
		req.Domain = publicEndpoint("rds", region)
	case "redis":
		req.Product = "R-kvstore"
		req.Version = "2015-01-01"
		req.Domain = publicEndpoint("r-kvstore", region)
	case "ecs":
		req.Product = "Ecs"
		req.Version = "2014-05-26"
		req.Domain = publicEndpoint("ecs", region)
	default:
		return errors.New("unsupported cloud service")
	}
	for k, v := range params {
		req.QueryParams[k] = v
	}
	resp, err := client.ProcessCommonRequest(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var coded interface{ ErrorCode() string }
		if errors.As(err, &coded) {
			code := coded.ErrorCode()
			if action == "DescribeDomainRecordInfo" && (code == "InvalidRecordId.NotFound" || code == "InvalidRecordId.NoExist") {
				return fmt.Errorf("%s: %w", action, cloud.ErrNotFound)
			}
			if strings.HasPrefix(code, "Forbidden") || code == "NoPermission" {
				prefix := map[string]string{"ecs": "ecs", "rds": "rds", "redis": "kvstore", "dns": "alidns"}[product]
				return fmt.Errorf("%s: %s。请核对 RAM 权限 %s:%s、授权资源范围及显式 Deny 策略。", action, code, prefix, action)
			}
			return fmt.Errorf("%s: %s", action, code)
		}
		return fmt.Errorf("%s failed (network or cloud service error); inspect remote state before retrying", action)
	}
	if err = json.Unmarshal(resp.GetHttpContentBytes(), out); err != nil {
		return fmt.Errorf("%s returned invalid response", action)
	}
	return nil
}
func (c *Client) targetParams(t cloud.Target) (map[string]string, error) {
	if t.ID == "" {
		return nil, errors.New("target ID is required")
	}
	if t.AccountID != "" && t.AccountID != c.account.ID {
		return nil, errors.New("target belongs to a different account")
	}
	region := t.Region
	if region == "" {
		region = c.account.Region
	}
	p := map[string]string{"RegionId": region}
	switch t.Kind {
	case "rds":
		p["DBInstanceId"] = t.ID
	case "redis":
		p["InstanceId"] = t.ID
	case "ecs":
		p["SecurityGroupId"] = t.ID
	default:
		return nil, errors.New("unsupported whitelist kind")
	}
	return p, nil
}
func splitIPs(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// publicEndpoint follows the published public endpoint tables, checked 2026-11-06.
// The pinned SDK's generated maps contain older VPC/internal routing entries, so
// use the current documented public exceptions for this desktop application.
// https://www.alibabacloud.com/help/en/redis/developer-reference/api-r-kvstore-2015-01-01-endpoint-redis
// https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-endpoint
// https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-endpoint
func publicEndpoint(service, region string) string {
	switch service {
	case "r-kvstore", "rds":
		if region == "cn-hangzhou" || region == "cn-hangzhou-finance" {
			return service + ".aliyuncs.com"
		}
	case "ecs":
		if region == "cn-hangzhou-finance" || region == "cn-north-2-gov-1" {
			return "ecs.aliyuncs.com"
		}
	}
	return service + "." + region + ".aliyuncs.com"
}
