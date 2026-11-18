package tencent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	cdb "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdb/v20170320"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	redis "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/redis/v20180412"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
	"yuntools/internal/cloud"
)

func normalizeWhitelistCIDR(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("IP 地址不能为空")
	}
	if strings.Contains(raw, "/") {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return "", errors.New("无效的 IP/CIDR 地址")
		}
		return p.Masked().String(), nil
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return "", errors.New("无效的 IP 地址")
	}
	if ip.Is4() {
		return ip.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}

func (c *Client) targetRegion(t cloud.Target) string {
	if strings.TrimSpace(t.Region) != "" {
		return strings.TrimSpace(t.Region)
	}
	return c.account.Region
}

func (c *Client) ListTargets(ctx context.Context, kind, region string) ([]cloud.Target, error) {
	if region == "" {
		region = c.account.Region
	}
	if region == "" {
		return nil, errors.New("region is required")
	}
	var out []cloud.Target
	switch kind {
	case "rds":
		cc, err := c.cdbClient(region)
		if err != nil {
			return nil, err
		}
		var offset uint64
		const limit uint64 = 100
		for {
			req := cdb.NewDescribeDBInstancesRequest()
			req.Offset = common.Uint64Ptr(offset)
			req.Limit = common.Uint64Ptr(limit)
			resp, err := cc.DescribeDBInstancesWithContext(ctx, req)
			if err != nil {
				return nil, err
			}
			if resp.Response == nil || len(resp.Response.Items) == 0 {
				break
			}
			for _, item := range resp.Response.Items {
				out = append(out, cloud.Target{
					AccountID: c.account.ID,
					Kind:      "rds",
					ID:        strVal(item.InstanceId),
					Name:      strVal(item.InstanceName),
					Region:    region,
					Group:     "default",
					Created:   parseTencentTime(strVal(item.CreateTime)),
				})
			}
			offset += uint64(len(resp.Response.Items))
			if uint64(len(resp.Response.Items)) < limit || (resp.Response.TotalCount != nil && int64(offset) >= *resp.Response.TotalCount) {
				break
			}
		}
	case "redis":
		rc, err := c.redisClient(region)
		if err != nil {
			return nil, err
		}
		var offset uint64
		const limit uint64 = 100
		for {
			req := redis.NewDescribeInstancesRequest()
			req.Offset = common.Uint64Ptr(offset)
			req.Limit = common.Uint64Ptr(limit)
			resp, err := rc.DescribeInstancesWithContext(ctx, req)
			if err != nil {
				return nil, err
			}
			if resp.Response == nil || len(resp.Response.InstanceSet) == 0 {
				break
			}
			for _, item := range resp.Response.InstanceSet {
				out = append(out, cloud.Target{
					AccountID: c.account.ID,
					Kind:      "redis",
					ID:        strVal(item.InstanceId),
					Name:      strVal(item.InstanceName),
					Region:    region,
					Group:     "default",
					Created:   parseTencentTime(strVal(item.Createtime)),
				})
			}
			offset += uint64(len(resp.Response.InstanceSet))
			if uint64(len(resp.Response.InstanceSet)) < limit || (resp.Response.TotalCount != nil && int64(offset) >= *resp.Response.TotalCount) {
				break
			}
		}
	case "ecs":
		vc, err := c.vpcClient(region)
		if err != nil {
			return nil, err
		}
		var offset uint64
		const limit uint64 = 100
		for {
			req := vpc.NewDescribeSecurityGroupsRequest()
			req.Offset = common.StringPtr(strconv.FormatUint(offset, 10))
			req.Limit = common.StringPtr(strconv.FormatUint(limit, 10))
			resp, err := vc.DescribeSecurityGroupsWithContext(ctx, req)
			if err != nil {
				return nil, err
			}
			if resp.Response == nil || len(resp.Response.SecurityGroupSet) == 0 {
				break
			}
			for _, sg := range resp.Response.SecurityGroupSet {
				out = append(out, cloud.Target{
					AccountID: c.account.ID,
					Kind:      "ecs",
					ID:        strVal(sg.SecurityGroupId),
					Name:      strVal(sg.SecurityGroupName),
					Region:    region,
					Group:     "default",
					Created:   parseTencentTime(strVal(sg.CreatedTime)),
				})
			}
			offset += uint64(len(resp.Response.SecurityGroupSet))
			if uint64(len(resp.Response.SecurityGroupSet)) < limit || (resp.Response.TotalCount != nil && offset >= *resp.Response.TotalCount) {
				break
			}
		}
	default:
		return nil, errors.New("unsupported whitelist kind")
	}
	return cloud.NewestFirst(out, func(t cloud.Target) time.Time { return t.Created }), nil
}

func (c *Client) resolveSecurityGroupID(ctx context.Context, t cloud.Target) (string, error) {
	region := c.targetRegion(t)
	switch t.Kind {
	case "ecs":
		if t.ID == "" {
			return "", errors.New("安全组 ID 不能为空")
		}
		return t.ID, nil
	case "rds":
		cc, err := c.cdbClient(region)
		if err != nil {
			return "", err
		}
		req := cdb.NewDescribeDBSecurityGroupsRequest()
		req.InstanceId = common.StringPtr(t.ID)
		resp, err := cc.DescribeDBSecurityGroupsWithContext(ctx, req)
		if err != nil {
			return "", err
		}
		if resp.Response == nil || len(resp.Response.Groups) == 0 || strVal(resp.Response.Groups[0].SecurityGroupId) == "" {
			return "", errors.New("该云数据库实例尚未绑定安全组，请先在腾讯云控制台绑定安全组或直接管理 VPC 安全组")
		}
		return strVal(resp.Response.Groups[0].SecurityGroupId), nil
	case "redis":
		rc, err := c.redisClient(region)
		if err != nil {
			return "", err
		}
		req := redis.NewDescribeInstanceSecurityGroupRequest()
		req.InstanceIds = common.StringPtrs([]string{t.ID})
		resp, err := rc.DescribeInstanceSecurityGroupWithContext(ctx, req)
		if err != nil {
			return "", err
		}
		if resp.Response != nil && len(resp.Response.InstanceSecurityGroupsDetail) > 0 {
			for _, detail := range resp.Response.InstanceSecurityGroupsDetail {
				if detail != nil && len(detail.SecurityGroupDetails) > 0 && strVal(detail.SecurityGroupDetails[0].SecurityGroupId) != "" {
					return strVal(detail.SecurityGroupDetails[0].SecurityGroupId), nil
				}
			}
		}
		return "", errors.New("该 Redis 实例尚未绑定安全组，请先在腾讯云控制台绑定安全组或直接管理 VPC 安全组")
	default:
		return "", errors.New("unsupported whitelist kind")
	}
}

func (c *Client) describePolicies(ctx context.Context, region, sgID string) (*vpc.SecurityGroupPolicySet, error) {
	vc, err := c.vpcClient(region)
	if err != nil {
		return nil, err
	}
	req := vpc.NewDescribeSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(sgID)
	resp, err := vc.DescribeSecurityGroupPoliciesWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil || resp.Response.SecurityGroupPolicySet == nil {
		return &vpc.SecurityGroupPolicySet{}, nil
	}
	return resp.Response.SecurityGroupPolicySet, nil
}

func (c *Client) ListRules(ctx context.Context, t cloud.Target) ([]string, error) {
	sgID, err := c.resolveSecurityGroupID(ctx, t)
	if err != nil {
		return nil, err
	}
	set, err := c.describePolicies(ctx, c.targetRegion(t), sgID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var rules []string
	for _, p := range set.Ingress {
		if p == nil || !strings.EqualFold(strVal(p.Action), "ACCEPT") {
			continue
		}
		addr := strVal(p.CidrBlock)
		if addr == "" {
			addr = strVal(p.Ipv6CidrBlock)
		}
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		rules = append(rules, addr)
	}
	return rules, nil
}

func (c *Client) ListSecurityGroupRules(ctx context.Context, t cloud.Target) ([]cloud.SecurityGroupRule, error) {
	sgID, err := c.resolveSecurityGroupID(ctx, t)
	if err != nil {
		return nil, err
	}
	set, err := c.describePolicies(ctx, c.targetRegion(t), sgID)
	if err != nil {
		return nil, err
	}
	var out []cloud.SecurityGroupRule
	appendDir := func(dir string, list []*vpc.SecurityGroupPolicy) {
		for _, p := range list {
			if p == nil {
				continue
			}
			addr := strVal(p.CidrBlock)
			if addr == "" {
				addr = strVal(p.Ipv6CidrBlock)
			}
			src, dst := addr, ""
			if dir == "egress" {
				src, dst = "", addr
			}
			out = append(out, cloud.SecurityGroupRule{
				Created:     parseTencentTime(strVal(p.ModifyTime)),
				ID:          strconv.FormatInt(i64Val(p.PolicyIndex), 10),
				Direction:   dir,
				Policy:      strings.ToLower(strVal(p.Action)),
				Protocol:    strVal(p.Protocol),
				Ports:       strVal(p.Port),
				Source:      src,
				Destination: dst,
				Priority:    strconv.FormatInt(i64Val(p.Priority), 10),
				Description: strVal(p.PolicyDescription),
			})
		}
	}
	appendDir("ingress", set.Ingress)
	appendDir("egress", set.Egress)
	return cloud.NewestFirst(out, func(r cloud.SecurityGroupRule) time.Time { return r.Created }), nil
}

func targetPolicySpec(t cloud.Target) (protocol, port string) {
	if t.Kind != "ecs" {
		return "ALL", "ALL"
	}
	protocol = strings.ToUpper(strings.TrimSpace(t.Protocol))
	if protocol == "" {
		protocol = "TCP"
	}
	port = strings.TrimSpace(t.Ports)
	if port == "" {
		return protocol, "ALL"
	}
	// Normalize Aliyun "22/22" or "8000/9000" style to Tencent "22" or "8000-9000"
	if strings.Contains(port, "/") {
		parts := strings.SplitN(port, "/", 2)
		if parts[0] == parts[1] {
			port = parts[0]
		} else {
			port = parts[0] + "-" + parts[1]
		}
	}
	return protocol, port
}

func (c *Client) AddIP(ctx context.Context, t cloud.Target, ip string) error {
	cidr, err := normalizeWhitelistCIDR(ip)
	if err != nil {
		return err
	}
	sgID, err := c.resolveSecurityGroupID(ctx, t)
	if err != nil {
		return err
	}
	region := c.targetRegion(t)
	protocol, port := targetPolicySpec(t)
	set, err := c.describePolicies(ctx, region, sgID)
	if err != nil {
		return err
	}
	isV6 := strings.Contains(cidr, ":")
	for _, p := range set.Ingress {
		if p == nil || !strings.EqualFold(strVal(p.Action), "ACCEPT") {
			continue
		}
		existing := strVal(p.CidrBlock)
		if isV6 {
			existing = strVal(p.Ipv6CidrBlock)
		}
		if strings.EqualFold(existing, cidr) && strings.EqualFold(strVal(p.Protocol), protocol) && strings.EqualFold(strVal(p.Port), port) {
			return nil
		}
	}
	policy := &vpc.SecurityGroupPolicy{
		Protocol:          common.StringPtr(protocol),
		Port:              common.StringPtr(port),
		Action:            common.StringPtr("ACCEPT"),
		PolicyDescription: common.StringPtr("yuntools"),
	}
	if isV6 {
		policy.Ipv6CidrBlock = common.StringPtr(cidr)
	} else {
		policy.CidrBlock = common.StringPtr(cidr)
	}
	vc, err := c.vpcClient(region)
	if err != nil {
		return err
	}
	req := vpc.NewCreateSecurityGroupPoliciesRequest()
	req.SecurityGroupId = common.StringPtr(sgID)
	req.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Ingress: []*vpc.SecurityGroupPolicy{policy},
	}
	_, err = vc.CreateSecurityGroupPoliciesWithContext(ctx, req)
	return err
}

func (c *Client) ownershipKey(t cloud.Target, sgID string) string {
	raw, _ := json.Marshal([]string{c.account.ID, t.Kind, t.ID, sgID, t.Protocol, t.Ports, t.ConfigID})
	sum := sha256.Sum256(raw)
	return "whitelist/tencent/owned/v1/" + hex.EncodeToString(sum[:])
}

func (c *Client) SyncIP(ctx context.Context, t cloud.Target, ip string, store cloud.SecretStore) error {
	if store == nil {
		return errors.New("secret store is required for dynamic whitelist sync")
	}
	cidr, err := normalizeWhitelistCIDR(ip)
	if err != nil {
		return err
	}
	sgID, err := c.resolveSecurityGroupID(ctx, t)
	if err != nil {
		return err
	}
	key := c.ownershipKey(t, sgID)
	var prev string
	if b, err := store.Get(key); err == nil {
		prev = strings.TrimSpace(string(b))
	}
	if err := c.AddIP(ctx, t, cidr); err != nil {
		return err
	}
	if err := store.Put(key, []byte(cidr)); err != nil {
		return err
	}
	if prev == "" || strings.EqualFold(prev, cidr) {
		return nil
	}
	region := c.targetRegion(t)
	protocol, port := targetPolicySpec(t)
	set, err := c.describePolicies(ctx, region, sgID)
	if err != nil {
		return err
	}
	isV6 := strings.Contains(prev, ":")
	for _, p := range set.Ingress {
		if p == nil || p.PolicyIndex == nil {
			continue
		}
		existing := strVal(p.CidrBlock)
		if isV6 {
			existing = strVal(p.Ipv6CidrBlock)
		}
		if strings.EqualFold(existing, prev) && strings.EqualFold(strVal(p.Protocol), protocol) && strings.EqualFold(strVal(p.Port), port) && strVal(p.PolicyDescription) == "yuntools" {
			vc, err := c.vpcClient(region)
			if err != nil {
				return err
			}
			delReq := vpc.NewDeleteSecurityGroupPoliciesRequest()
			delReq.SecurityGroupId = common.StringPtr(sgID)
			delReq.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
				Ingress: []*vpc.SecurityGroupPolicy{{PolicyIndex: p.PolicyIndex}},
			}
			if _, err := vc.DeleteSecurityGroupPoliciesWithContext(ctx, delReq); err != nil {
				return fmt.Errorf("已添加新 IP，但清理旧规则 %s 失败: %w", prev, err)
			}
			break
		}
	}
	return nil
}
