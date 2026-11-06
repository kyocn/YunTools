package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"yuntools/internal/cloud"
)

func (c *Client) ListTargets(ctx context.Context, kind, region string) ([]cloud.Target, error) {
	if region == "" {
		region = c.account.Region
	}
	if !regionPattern.MatchString(region) {
		return nil, errors.New("invalid region")
	}
	var out []cloud.Target
	action := map[string]string{"rds": "DescribeDBInstances", "redis": "DescribeInstances", "ecs": "DescribeSecurityGroups"}[kind]
	if action == "" {
		return nil, errors.New("unsupported whitelist kind")
	}
	for page := 1; ; page++ {
		var r struct {
			TotalCount int
			Items      struct {
				DBInstance []struct{ DBInstanceId, DBInstanceDescription, CreateTime string }
			}
			Instances struct {
				KVStoreInstance []struct{ InstanceId, InstanceName, CreateTime string }
			}
			SecurityGroups struct {
				SecurityGroup []struct{ SecurityGroupId, SecurityGroupName, CreationTime string }
			}
		}
		if err := c.call(ctx, kind, action, map[string]string{"RegionId": region, "PageNumber": strconv.Itoa(page), "PageSize": "100"}, &r); err != nil {
			return nil, err
		}
		before := len(out)
		add := func(id, name, created string) {
			out = append(out, cloud.Target{AccountID: c.account.ID, Kind: kind, ID: id, Name: name, Region: region, Group: "default", Created: parseCloudTime(created)})
		}
		for _, v := range r.Items.DBInstance {
			add(v.DBInstanceId, v.DBInstanceDescription, v.CreateTime)
		}
		for _, v := range r.Instances.KVStoreInstance {
			add(v.InstanceId, v.InstanceName, v.CreateTime)
		}
		for _, v := range r.SecurityGroups.SecurityGroup {
			add(v.SecurityGroupId, v.SecurityGroupName, v.CreationTime)
		}
		if len(out) == before || len(out) >= r.TotalCount {
			break
		}
	}
	return cloud.NewestFirst(out, func(t cloud.Target) time.Time { return t.Created }), nil
}

type permission struct {
	CreateTime, SourcePortRange, NicType                                     string
	SourceCidrIp, Ipv6SourceCidrIp, IpProtocol, PortRange, Policy, Direction string
	SecurityGroupRuleId, DestCidrIp, Ipv6DestCidrIp, Description             string
	SourceGroupId, DestGroupId, SourcePrefixListId, DestPrefixListId         string
	Priority                                                                 json.Number
}

func (c *Client) permissions(ctx context.Context, t cloud.Target) ([]permission, error) {
	return c.permissionsDirection(ctx, t, "ingress")
}

func (c *Client) permissionsDirection(ctx context.Context, t cloud.Target, direction string) ([]permission, error) {
	p, err := c.targetParams(t)
	if err != nil {
		return nil, err
	}
	p["Direction"] = direction
	p["MaxResults"] = "1000"
	var out []permission
	for {
		var r struct {
			NextToken   string
			Permissions struct{ Permission []permission }
		}
		if err = c.call(ctx, "ecs", "DescribeSecurityGroupAttribute", p, &r); err != nil {
			return nil, err
		}
		out = append(out, r.Permissions.Permission...)
		if r.NextToken == "" {
			break
		}
		if r.NextToken == p["NextToken"] {
			return nil, errors.New("repeated security group page token")
		}
		p["NextToken"] = r.NextToken
	}
	return out, nil
}

func ruleAddresses(cidr, ipv6, group, prefix string) string {
	var parts []string
	for _, v := range []string{cidr, ipv6} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	if group != "" {
		parts = append(parts, "安全组 "+group)
	}
	if prefix != "" {
		parts = append(parts, "地址前缀列表 "+prefix)
	}
	return strings.Join(parts, " · ")
}

func (c *Client) ListSecurityGroupRules(ctx context.Context, t cloud.Target) ([]cloud.SecurityGroupRule, error) {
	if t.Kind != "ecs" {
		return nil, errors.New("请选择 ECS 安全组查看详细规则")
	}
	rules, err := c.permissionsDirection(ctx, t, "all")
	if err != nil {
		return nil, err
	}
	out := make([]cloud.SecurityGroupRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, cloud.SecurityGroupRule{Created: parseCloudTime(r.CreateTime), ID: r.SecurityGroupRuleId, Direction: r.Direction, Policy: r.Policy,
			Protocol: r.IpProtocol, Ports: r.PortRange, Priority: r.Priority.String(), Description: r.Description,
			Source:      ruleAddresses(r.SourceCidrIp, r.Ipv6SourceCidrIp, r.SourceGroupId, r.SourcePrefixListId),
			Destination: ruleAddresses(r.DestCidrIp, r.Ipv6DestCidrIp, r.DestGroupId, r.DestPrefixListId)})
	}
	return cloud.NewestFirst(out, func(r cloud.SecurityGroupRule) time.Time { return r.Created }), nil
}
func (c *Client) ListRules(ctx context.Context, t cloud.Target) ([]string, error) {
	p, err := c.targetParams(t)
	if err != nil {
		return nil, err
	}
	var out []string
	switch t.Kind {
	case "rds":
		var r struct {
			Items struct {
				DBInstanceIPArray []struct{ DBInstanceIPArrayName, SecurityIPList string }
			}
		}
		if err = c.call(ctx, "rds", "DescribeDBInstanceIPArrayList", p, &r); err != nil {
			return nil, err
		}
		for _, v := range r.Items.DBInstanceIPArray {
			if t.Group == "" || v.DBInstanceIPArrayName == t.Group {
				out = append(out, splitIPs(v.SecurityIPList)...)
			}
		}
	case "redis":
		var r struct {
			SecurityIpGroups struct {
				SecurityIpGroup []struct{ SecurityIpGroupName, SecurityIpList string }
			}
		}
		if err = c.call(ctx, "redis", "DescribeSecurityIps", p, &r); err != nil {
			return nil, err
		}
		for _, v := range r.SecurityIpGroups.SecurityIpGroup {
			if t.Group == "" || v.SecurityIpGroupName == t.Group {
				out = append(out, splitIPs(v.SecurityIpList)...)
			}
		}
	case "ecs":
		rules, err := c.permissions(ctx, t)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			source := r.SourceCidrIp
			if source == "" {
				source = r.Ipv6SourceCidrIp
			}
			out = append(out, fmt.Sprintf("%s %s %s %s", r.Policy, r.IpProtocol, r.PortRange, source))
		}
	}
	return out, nil
}
func validatePorts(t cloud.Target) error {
	if t.Protocol != "tcp" && t.Protocol != "udp" {
		return errors.New("explicit tcp or udp protocol is required")
	}
	parts := strings.Split(t.Ports, "/")
	if len(parts) != 2 {
		return errors.New("ports must be start/end")
	}
	lo, e1 := strconv.Atoi(parts[0])
	hi, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || lo < 1 || hi > 65535 || lo > hi {
		return errors.New("ports must be between 1 and 65535")
	}
	return nil
}
func hasIP(rules []string, ip netip.Addr) bool {
	for _, r := range rules {
		if a, e := netip.ParseAddr(r); e == nil && a.Unmap() == ip.Unmap() {
			return true
		}
		if p, e := netip.ParsePrefix(r); e == nil && p.Bits() == p.Addr().BitLen() && p.Addr().Unmap() == ip.Unmap() {
			return true
		}
	}
	return false
}
func matchesRule(r permission, t cloud.Target, ip netip.Addr) bool {
	if !strings.EqualFold(r.Policy, "accept") || (r.Direction != "" && !strings.EqualFold(r.Direction, "ingress")) || !strings.EqualFold(r.IpProtocol, t.Protocol) || r.PortRange != t.Ports {
		return false
	}
	return hasIP([]string{r.SourceCidrIp, r.Ipv6SourceCidrIp}, ip)
}
func (c *Client) AddIP(ctx context.Context, t cloud.Target, value string) error {
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("a single valid IP address is required")
	}
	ip = ip.Unmap()
	p, err := c.targetParams(t)
	if err != nil {
		return err
	}
	if t.Group == "" {
		t.Group = "default"
	}
	if t.Kind == "ecs" {
		if err = validatePorts(t); err != nil {
			return err
		}
		before, err := c.permissions(ctx, t)
		if err != nil {
			return err
		}
		for _, r := range before {
			if matchesRule(r, t, ip) {
				return nil
			}
		}
		p["IpProtocol"] = t.Protocol
		p["PortRange"] = t.Ports
		p["Policy"] = "accept"
		p["Priority"] = "1"
		p["Description"] = "YunTools appended IP"
		if ip.Is4() {
			p["SourceCidrIp"] = ip.String() + "/32"
		} else {
			p["Ipv6SourceCidrIp"] = ip.String() + "/128"
		}
		var result map[string]any
		if err = c.call(ctx, "ecs", "AuthorizeSecurityGroup", p, &result); err != nil {
			return err
		}
		after, err := c.permissions(ctx, t)
		if err != nil {
			return err
		}
		for _, r := range after {
			if matchesRule(r, t, ip) {
				return nil
			}
		}
		return errors.New("新增请求已提交，但暂未查询到匹配规则；请先刷新规则列表，勿重复添加")
	}
	before, err := c.ListRules(ctx, t)
	if err != nil {
		return err
	}
	if hasIP(before, ip) {
		return nil
	}
	p["ModifyMode"] = "Append"
	p["SecurityIps"] = ip.String()
	if t.Kind == "rds" {
		p["DBInstanceIPArrayName"] = t.Group
	} else {
		p["SecurityIpGroupName"] = t.Group
	}
	var result map[string]any
	if err = c.call(ctx, t.Kind, "ModifySecurityIps", p, &result); err != nil {
		return err
	}
	after, err := c.ListRules(ctx, t)
	if err != nil {
		return err
	}
	if !hasIP(after, ip) {
		return errors.New("appended IP not visible yet; refresh before retry")
	}
	for _, old := range before {
		found := false
		for _, v := range after {
			if v == old {
				found = true
				break
			}
		}
		if !found {
			return errors.New("previous whitelist entry no longer present; inspect concurrent cloud changes")
		}
	}
	return nil
}

// PublicIP measures the IPv4 egress to a domestic HTTPS endpoint. HTTP proxies
// are bypassed, but system VPN/TUN routing can still affect the observed address.
func PublicIP(ctx context.Context) (string, error) {
	return fetchPublicIP(ctx, &http.Client{Timeout: 10 * time.Second}, "https://myip.ipip.net")
}
func fetchPublicIP(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	// Clone instead of changing the cloud SDKs' shared client or transport.
	direct := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		transport = transport.Clone()
		transport.Proxy = nil
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
		direct.Transport = transport
		defer transport.CloseIdleConnections()
	}
	direct.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if direct.Timeout <= 0 || direct.Timeout > 10*time.Second {
		direct.Timeout = 10 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	if req.URL.Scheme != "https" {
		return "", errors.New("public IP service must use HTTPS")
	}
	resp, err := direct.Do(req)
	if err != nil {
		return "", errors.New("公网 IPv4 检测服务不可用，请手动填写实际连接出口 IP；未回退到代理出口")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("public IP service returned an error")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if err != nil {
		return "", err
	}
	if len(data) > 1024 {
		return "", errors.New("public IP service response too large")
	}
	value := strings.TrimSpace(string(data))
	if strings.HasPrefix(value, "当前 IP：") {
		address, location, ok := strings.Cut(strings.TrimPrefix(value, "当前 IP："), "来自于：")
		if !ok || strings.TrimSpace(location) == "" {
			return "", errors.New("public IP service returned an invalid response")
		}
		value = strings.TrimSpace(address)
	}
	ip, err := netip.ParseAddr(value)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.Zone() != "" ||
		netip.MustParsePrefix("198.18.0.0/15").Contains(ip) || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return "", errors.New("public IP service returned an invalid public IP")
	}
	return ip.Unmap().String(), nil
}
