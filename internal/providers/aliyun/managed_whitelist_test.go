package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"yuntools/internal/cloud"
)

type managedStore struct {
	mu     sync.Mutex
	values map[string][]byte
	fail   bool
}

func (s *managedStore) Get(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[k]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}
func (s *managedStore) Put(k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("store unavailable")
	}
	if s.values == nil {
		s.values = map[string][]byte{}
	}
	s.values[k] = append([]byte(nil), v...)
	return nil
}
func (s *managedStore) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, k)
	return nil
}

type managedCloud struct {
	t                    *testing.T
	rules                []permission
	groups               map[string][]string
	calls                []string
	writes               int
	failAction, failMode string
	applyThenFail        bool
	serial               int
}

func (f *managedCloud) rpc(ctx context.Context, product, action string, p map[string]string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.calls = append(f.calls, action+":"+p["ModifyMode"])
	fail := f.failAction == action && (f.failMode == "" || f.failMode == p["ModifyMode"])
	write := action == "AuthorizeSecurityGroup" || action == "RevokeSecurityGroup" || action == "ModifySecurityIps"
	if write {
		f.writes++
	}
	if fail && !f.applyThenFail {
		return fmt.Errorf("%s: Forbidden.RAM", action)
	}
	var body any = map[string]any{}
	switch action {
	case "DescribeSecurityGroupAttribute":
		body = map[string]any{"Permissions": map[string]any{"Permission": f.rules}}
	case "AuthorizeSecurityGroup":
		if p["Description"] == "" || p["Description"] == "YunTools appended IP" {
			f.t.Fatal("missing managed ownership marker")
		}
		f.serial++
		f.rules = append(f.rules, permission{SecurityGroupRuleId: fmt.Sprintf("sgr-%d", f.serial), SourceCidrIp: p["SourceCidrIp"], Ipv6SourceCidrIp: p["Ipv6SourceCidrIp"], IpProtocol: p["IpProtocol"], PortRange: p["PortRange"], Policy: "Accept", Direction: "ingress", Priority: "1", Description: p["Description"]})
	case "RevokeSecurityGroup":
		id := p["SecurityGroupRuleId.1"]
		if id == "" || p["SourceCidrIp"] != "" {
			f.t.Fatal("revoke must use exact rule ID")
		}
		for i, r := range f.rules {
			if r.SecurityGroupRuleId == id {
				f.rules = append(f.rules[:i], f.rules[i+1:]...)
				break
			}
		}
	case "DescribeDBInstanceIPArrayList":
		var groups []map[string]string
		for name, ips := range f.groups {
			groups = append(groups, map[string]string{"DBInstanceIPArrayName": name, "SecurityIPList": strings.Join(ips, ","), "WhitelistNetworkType": "MIX"})
		}
		body = map[string]any{"Items": map[string]any{"DBInstanceIPArray": groups}}
	case "DescribeSecurityIps":
		var groups []map[string]string
		for name, ips := range f.groups {
			groups = append(groups, map[string]string{"SecurityIpGroupName": name, "SecurityIpList": strings.Join(ips, ",")})
		}
		body = map[string]any{"SecurityIpGroups": map[string]any{"SecurityIpGroup": groups}}
	case "ModifySecurityIps":
		group := p["DBInstanceIPArrayName"]
		if product == "redis" {
			group = p["SecurityIpGroupName"]
		}
		if !strings.HasPrefix(group, "yuntools_") || len(group) > 32 {
			f.t.Fatalf("non-managed group write %q", group)
		}
		if f.groups == nil {
			f.groups = map[string][]string{}
		}
		switch p["ModifyMode"] {
		case "Append":
			exists := false
			for _, v := range f.groups[group] {
				if v == p["SecurityIps"] {
					exists = true
				}
			}
			if !exists {
				f.groups[group] = append(f.groups[group], p["SecurityIps"])
			}
		case "Delete":
			var kept []string
			for _, v := range f.groups[group] {
				if v != p["SecurityIps"] {
					kept = append(kept, v)
				}
			}
			if len(kept) == 0 {
				f.t.Fatal("attempted empty whitelist")
			}
			f.groups[group] = kept
		default:
			f.t.Fatal("whole-list replacement prohibited")
		}
	default:
		f.t.Fatalf("unexpected RPC %s", action)
	}
	if fail {
		return errors.New("network response lost")
	}
	data, _ := json.Marshal(body)
	return json.Unmarshal(data, out)
}
func managedFixture(t *testing.T, kind string) (*Client, *managedCloud, *managedStore, cloud.Target) {
	f := &managedCloud{t: t, groups: map[string][]string{"default": {"10.0.0.1"}}}
	c := &Client{account: cloud.Account{ID: "a", Region: "cn-shanghai"}, call: f.rpc}
	target := cloud.Target{ConfigID: "0123456789abcdef0123456789abcdef", AccountID: "a", Kind: kind, ID: "resource-test", Region: "cn-shanghai", Group: "default", Protocol: "tcp", Ports: "22/22"}
	return c, f, &managedStore{}, target
}
func syncManaged(t *testing.T, c *Client, target cloud.Target, ip string, s cloud.SecretStore) {
	t.Helper()
	if err := c.SyncIP(context.Background(), target, ip, s); err != nil {
		t.Fatal(err)
	}
}
func foreignRule(id, ip string) permission {
	return permission{SecurityGroupRuleId: id, SourceCidrIp: ip + "/32", IpProtocol: "TCP", PortRange: "22/22", Policy: "Accept", Direction: "ingress", Description: "YunTools appended IP", Priority: "1"}
}

// Dropping ownership checks would delete the legacy/manual rule; removing new-before-old ordering would revoke access on an add failure.
func TestManagedECSRotationPreservesForeignAndRetriesCleanup(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	f.rules = []permission{foreignRule("legacy", "8.8.8.8")}
	syncManaged(t, c, target, "1.1.1.1", s)
	oldID := f.rules[1].SecurityGroupRuleId
	f.failAction = "AuthorizeSecurityGroup"
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("expected permission error")
	}
	if len(f.rules) != 2 || f.rules[1].SecurityGroupRuleId != oldID {
		t.Fatal("removed old rule before successful add")
	}
	f.failAction = "RevokeSecurityGroup"
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("expected cleanup error")
	}
	if len(f.rules) != 3 {
		t.Fatal("cleanup failure lost rules")
	}
	f.failAction = ""
	syncManaged(t, c, target, "9.9.9.9", s)
	if len(f.rules) != 2 || f.rules[0].SecurityGroupRuleId != "legacy" || f.rules[1].SourceCidrIp != "9.9.9.9/32" {
		t.Fatalf("bad rules %+v", f.rules)
	}
	writes := f.writes
	syncManaged(t, c, target, "9.9.9.9", s)
	if f.writes != writes {
		t.Fatal("unchanged IP wrote again")
	}
	syncManaged(t, c, target, "8.8.8.8", s)
	syncManaged(t, c, target, "4.4.4.4", s)
	if f.rules[0].SecurityGroupRuleId != "legacy" {
		t.Fatal("adopted foreign rule")
	}
}
func TestManagedGroupsRotateOnlyOwnedEntries(t *testing.T) {
	for _, kind := range []string{"rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			syncManaged(t, c, target, "1.1.1.1", s)
			var group string
			for g := range f.groups {
				if g != "default" {
					group = g
				}
			}
			f.groups[group] = append(f.groups[group], "10.0.0.2")
			f.failAction = "ModifySecurityIps"
			f.failMode = "Append"
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("missing error")
			}
			if !reflect.DeepEqual(f.groups[group], []string{"1.1.1.1", "10.0.0.2"}) {
				t.Fatal("old IP lost on failed new write")
			}
			f.failMode = "Delete"
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("missing cleanup error")
			}
			f.failAction = ""
			syncManaged(t, c, target, "9.9.9.9", s)
			if !reflect.DeepEqual(f.groups[group], []string{"10.0.0.2", "9.9.9.9"}) || !reflect.DeepEqual(f.groups["default"], []string{"10.0.0.1"}) {
				t.Fatalf("unrelated entries lost: %v", f.groups)
			}
			writes := f.writes
			syncManaged(t, c, target, "9.9.9.9", s)
			if writes != f.writes {
				t.Fatal("not idempotent")
			}
			// Identical foreign source in our own group must also remain unowned.
			syncManaged(t, c, target, "10.0.0.2", s)
			syncManaged(t, c, target, "4.4.4.4", s)
			if !reflect.DeepEqual(f.groups[group], []string{"10.0.0.2", "4.4.4.4"}) {
				t.Fatal("foreign source adopted")
			}
		})
	}
}
func TestManagedGroupsRefusePreexistingWithoutJournal(t *testing.T) {
	c, f, s, target := managedFixture(t, "rds")
	syncManaged(t, c, target, "1.1.1.1", s)
	writes := f.writes
	fresh := &managedStore{}
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", fresh); err == nil {
		t.Fatal("adopted pre-existing group")
	}
	if writes != f.writes {
		t.Fatal("mutated pre-existing group")
	}
}
func TestManagedScopeChangesFailClosed(t *testing.T) {
	for _, change := range []func(*cloud.Target){func(v *cloud.Target) { v.ID = "other" }, func(v *cloud.Target) { v.Group = "other" }, func(v *cloud.Target) { v.Ports = "80/80" }, func(v *cloud.Target) { v.Region = "cn-hangzhou" }} {
		c, f, s, target := managedFixture(t, "ecs")
		syncManaged(t, c, target, "1.1.1.1", s)
		writes := f.writes
		change(&target)
		if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
			t.Fatal("scope change accepted")
		}
		if writes != f.writes {
			t.Fatal("scope change wrote cloud")
		}
	}
}
func TestManagedTwoTargetsShareIPWithoutSharingGroup(t *testing.T) {
	c, f, s, target := managedFixture(t, "redis")
	syncManaged(t, c, target, "1.1.1.1", s)
	other := target
	other.ConfigID = "abcdef0123456789abcdef0123456789"
	syncManaged(t, c, other, "1.1.1.1", s)
	syncManaged(t, c, target, "9.9.9.9", s)
	counts := map[string]int{}
	for name, ips := range f.groups {
		if name != "default" {
			for _, ip := range ips {
				counts[ip]++
			}
		}
	}
	if counts["1.1.1.1"] != 1 || counts["9.9.9.9"] != 1 || len(f.groups) != 3 {
		t.Fatalf("shared ownership: %v", f.groups)
	}
}

func TestManagedECSAmbiguousAddRecoversUniqueMarker(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	syncManaged(t, c, target, "1.1.1.1", s)
	f.failAction = "AuthorizeSecurityGroup"
	f.applyThenFail = true
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("lost response must surface")
	}
	if len(f.rules) != 2 {
		t.Fatal("old rule removed on ambiguous add")
	}
	f.failAction = ""
	syncManaged(t, c, target, "9.9.9.9", s)
	if len(f.rules) != 1 || f.rules[0].SourceCidrIp != "9.9.9.9/32" || f.writes != 3 {
		t.Fatalf("retry failed %+v; writes %d", f.rules, f.writes)
	}
}
func TestManagedGroupsAmbiguousCreationNeverAdopts(t *testing.T) {
	for _, kind := range []string{"rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			f.failAction = "ModifySecurityIps"
			f.applyThenFail = true
			if err := c.SyncIP(context.Background(), target, "1.1.1.1", s); err == nil {
				t.Fatal("lost creation response must surface")
			}
			f.failAction = ""
			writes := f.writes
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("adopted group after unknown creation result")
			}
			if f.writes != writes {
				t.Fatal("ambiguous group mutated")
			}
		})
	}
}
func TestManagedGroupsAmbiguousAppendStaysUnowned(t *testing.T) {
	c, f, s, target := managedFixture(t, "rds")
	syncManaged(t, c, target, "1.1.1.1", s)
	f.failAction = "ModifySecurityIps"
	f.failMode = "Append"
	f.applyThenFail = true
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("lost response must surface")
	}
	f.failAction = ""
	syncManaged(t, c, target, "9.9.9.9", s)
	syncManaged(t, c, target, "4.4.4.4", s)
	for name, ips := range f.groups {
		if name != "default" && !reflect.DeepEqual(ips, []string{"9.9.9.9", "4.4.4.4"}) {
			t.Fatalf("adopted ambiguous append: %v", ips)
		}
	}
}
func TestManagedStaleECSOwnershipNeverDeletesReplacement(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	syncManaged(t, c, target, "1.1.1.1", s)
	f.rules = []permission{foreignRule("replacement", "1.1.1.1")}
	syncManaged(t, c, target, "9.9.9.9", s)
	if len(f.rules) != 2 || f.rules[0].SecurityGroupRuleId != "replacement" {
		t.Fatal("deleted unowned replacement")
	}
}
func TestManagedECSMetadataChangesFailClosed(t *testing.T) {
	for _, change := range []func(*permission){
		func(r *permission) { r.Description = "edited" }, func(r *permission) { r.Priority = "2" }, func(r *permission) { r.PortRange = "80/80" }, func(r *permission) { r.SourcePortRange = "123/123" }, func(r *permission) { r.Ipv6SourceCidrIp = "2001:db8::1/128" },
	} {
		c, f, s, target := managedFixture(t, "ecs")
		syncManaged(t, c, target, "1.1.1.1", s)
		change(&f.rules[0])
		writes := f.writes
		if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
			t.Fatal("modified owned rule accepted")
		}
		if f.writes != writes {
			t.Fatal("write after rule metadata change")
		}
	}
}
func TestManagedRejectsInvalidAndCanceledBeforeRPC(t *testing.T) {
	for _, kind := range []string{"ecs", "rds", "redis"} {
		c, f, s, target := managedFixture(t, kind)
		for _, ip := range []string{"garbage", "0.0.0.0", "224.0.0.1", "1.1.1.1/32"} {
			if err := c.SyncIP(context.Background(), target, ip, s); err == nil {
				t.Fatal("invalid address accepted")
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.SyncIP(ctx, target, "1.1.1.1", s); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if kind != "ecs" {
			if err := c.SyncIP(context.Background(), target, "2001:db8::1", s); err == nil {
				t.Fatal("unsupported IPv6 accepted")
			}
		}
		target.ConfigID = ""
		if err := c.SyncIP(context.Background(), target, "1.1.1.1", s); err == nil {
			t.Fatal("missing config ID accepted")
		}
		if len(f.calls) != 0 {
			t.Fatal("invalid input reached API")
		}
	}
}
func TestManagedStoreFailurePreventsCloudWrites(t *testing.T) {
	for _, kind := range []string{"ecs", "rds", "redis"} {
		c, f, s, target := managedFixture(t, kind)
		s.fail = true
		if err := c.SyncIP(context.Background(), target, "1.1.1.1", s); err == nil {
			t.Fatal("store failure ignored")
		}
		if f.writes != 0 {
			t.Fatal("cloud write without durable journal")
		}
	}
}
func TestManagedMissingReadPermissionNeverWrites(t *testing.T) {
	for kind, action := range map[string]string{"ecs": "DescribeSecurityGroupAttribute", "rds": "DescribeDBInstanceIPArrayList", "redis": "DescribeSecurityIps"} {
		c, f, s, target := managedFixture(t, kind)
		f.failAction = action
		if err := c.SyncIP(context.Background(), target, "1.1.1.1", s); err == nil || !strings.Contains(err.Error(), "Forbidden.RAM") {
			t.Fatalf("permission error lost: %v", err)
		}
		if f.writes != 0 {
			t.Fatal("write despite unreadable cloud state")
		}
	}
}
func TestManagedGroupReservedNameCannotBecomeForeign(t *testing.T) {
	c, f, s, target := managedFixture(t, "rds")
	syncManaged(t, c, target, "1.1.1.1", s)
	// Simulate crash after persisting the reserved name but before persisting a
	// creation intent. A third party has created that name before the retry.
	for key, data := range s.values {
		var j managedJournal
		if err := json.Unmarshal(data, &j); err != nil {
			t.Fatal(err)
		}
		j.GroupOwned = false
		j.GroupNetwork = ""
		j.Entries = nil
		j.Pending = nil
		s.values[key], _ = json.Marshal(j)
	}
	writes := f.writes
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("reserved name adopted a pre-existing group")
	}
	if f.writes != writes {
		t.Fatal("foreign reserved name mutated")
	}
}
func TestManagedSourceIPChangesDoNotChangeOwnership(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	target.SourceIP = "1.1.1.1"
	syncManaged(t, c, target, "1.1.1.1", s)
	target.SourceIP = "9.9.9.9"
	target.Automatic = true
	syncManaged(t, c, target, "9.9.9.9", s)
	if len(f.rules) != 1 || f.rules[0].SourceCidrIp != "9.9.9.9/32" {
		t.Fatal("source setting changed ownership scope")
	}
}

func TestManagedAcknowledgedGroupWriteRecoversAfterReadFailure(t *testing.T) {
	for _, kind := range []string{"rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			base := c.call
			c.call = func(ctx context.Context, product, action string, p map[string]string, out any) error {
				err := base(ctx, product, action, p, out)
				if action == "ModifySecurityIps" && err == nil {
					if kind == "rds" {
						f.failAction = "DescribeDBInstanceIPArrayList"
					} else {
						f.failAction = "DescribeSecurityIps"
					}
				}
				return err
			}
			if err := c.SyncIP(context.Background(), target, "1.1.1.1", s); err == nil {
				t.Fatal("verification read failure ignored")
			}
			c.call = base
			f.failAction = ""
			syncManaged(t, c, target, "1.1.1.1", s)
			syncManaged(t, c, target, "9.9.9.9", s)
			for name, ips := range f.groups {
				if name != "default" && !reflect.DeepEqual(ips, []string{"9.9.9.9"}) {
					t.Fatalf("acknowledged creation not recovered: %v", ips)
				}
			}
		})
	}
}
func TestManagedCommittedDeleteWithLostResponseIsRetryable(t *testing.T) {
	for _, kind := range []string{"ecs", "rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			syncManaged(t, c, target, "1.1.1.1", s)
			if kind == "ecs" {
				f.failAction = "RevokeSecurityGroup"
			} else {
				f.failAction = "ModifySecurityIps"
				f.failMode = "Delete"
			}
			f.applyThenFail = true
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("lost delete response ignored")
			}
			writes := f.writes
			f.failAction = ""
			syncManaged(t, c, target, "9.9.9.9", s)
			if writes != f.writes {
				t.Fatal("retry repeated already committed deletion")
			}
		})
	}
}
func TestManagedCancellationBeforeCleanupPreservesOld(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	syncManaged(t, c, target, "1.1.1.1", s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := c.call
	c.call = func(ctx context.Context, product, action string, p map[string]string, out any) error {
		err := base(ctx, product, action, p, out)
		if action == "AuthorizeSecurityGroup" {
			cancel()
		}
		return err
	}
	if err := c.SyncIP(ctx, target, "9.9.9.9", s); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if len(f.rules) != 2 {
		t.Fatal("old rule removed after cancellation")
	}
}
func TestManagedParallelClientsSerializeScope(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	other := &Client{account: c.account, call: f.rpc}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, client := range []*Client{c, other} {
		go func(client *Client) { <-start; errs <- client.SyncIP(context.Background(), target, "1.1.1.1", s) }(client)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if len(f.rules) != 1 || f.writes != 1 {
		t.Fatalf("concurrent clients duplicated rule: %+v", f.rules)
	}
}
func TestManagedScopeLockWaitCanBeCanceled(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	once := sync.Once{}
	base := c.call
	c.call = func(ctx context.Context, product, action string, p map[string]string, out any) error {
		once.Do(func() { close(entered); <-release })
		return base(ctx, product, action, p, out)
	}
	go func() { done <- c.SyncIP(context.Background(), target, "1.1.1.1", s) }()
	<-entered
	other := &Client{account: c.account, call: f.rpc}
	otherTarget := target
	otherTarget.ConfigID = "abcdef0123456789abcdef0123456789"
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- other.SyncIP(ctx, otherTarget, "9.9.9.9", s) }()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Errorf("wait did not cancel: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 1 {
		t.Fatal("canceled waiter reached cloud")
	}
}
func TestManagedGroupLostOwnershipOrMetadataFailsClosed(t *testing.T) {
	for _, kind := range []string{"rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			syncManaged(t, c, target, "1.1.1.1", s)
			for name := range f.groups {
				if name != "default" {
					delete(f.groups, name)
				}
			}
			writes := f.writes
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("disappeared group silently recreated")
			}
			if f.writes != writes {
				t.Fatal("write after group disappearance")
			}
		})
	}
}

func TestManagedMalformedJournalNeverRebindsScope(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	syncManaged(t, c, target, "1.1.1.1", s)
	for k := range s.values {
		s.values[k] = []byte(`{}`)
	}
	calls := len(f.calls)
	target.ID = "other-resource"
	if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
		t.Fatal("empty journal accepted as ownership in a new scope")
	}
	if len(f.calls) != calls {
		t.Fatal("malformed journal reached cloud")
	}
}
func TestManagedRejectsMappedUnspecifiedAndMulticast(t *testing.T) {
	c, f, s, target := managedFixture(t, "ecs")
	for _, ip := range []string{"::ffff:0.0.0.0", "::ffff:224.0.0.1"} {
		if err := c.SyncIP(context.Background(), target, ip, s); err == nil {
			t.Fatalf("accepted %s", ip)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("invalid mapped IP reached cloud")
	}
}

func TestManagedStoreFailureAfterAddRetainsOldAndRecoversConservatively(t *testing.T) {
	for _, kind := range []string{"ecs", "rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			syncManaged(t, c, target, "1.1.1.1", s)
			base := c.call
			c.call = func(ctx context.Context, product, action string, p map[string]string, out any) error {
				err := base(ctx, product, action, p, out)
				if action == "AuthorizeSecurityGroup" || (action == "ModifySecurityIps" && p["ModifyMode"] == "Append") {
					s.fail = true
				}
				return err
			}
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("post-add durable write failure ignored")
			}
			if kind == "ecs" {
				if len(f.rules) != 2 {
					t.Fatal("old ECS access removed after journal failure")
				}
			} else {
				for name, ips := range f.groups {
					if name != "default" && !reflect.DeepEqual(ips, []string{"1.1.1.1", "9.9.9.9"}) {
						t.Fatalf("old group access removed: %v", ips)
					}
				}
			}
			s.fail = false
			c.call = base
			syncManaged(t, c, target, "9.9.9.9", s)
			syncManaged(t, c, target, "4.4.4.4", s)
			if kind == "ecs" {
				if len(f.rules) != 1 {
					t.Fatal("ECS pending marker did not recover ownership")
				}
			} else {
				for name, ips := range f.groups {
					if name != "default" && !reflect.DeepEqual(ips, []string{"9.9.9.9", "4.4.4.4"}) {
						t.Fatalf("group adopted unacknowledged address: %v", ips)
					}
				}
			}
		})
	}
}
func TestManagedGroupMetadataMutationBlocksWrites(t *testing.T) {
	for _, kind := range []string{"rds", "redis"} {
		t.Run(kind, func(t *testing.T) {
			c, f, s, target := managedFixture(t, kind)
			syncManaged(t, c, target, "1.1.1.1", s)
			base := c.call
			c.call = func(ctx context.Context, product, action string, p map[string]string, out any) error {
				if action != "DescribeDBInstanceIPArrayList" && action != "DescribeSecurityIps" {
					return base(ctx, product, action, p, out)
				}
				var response map[string]any
				if err := base(ctx, product, action, p, &response); err != nil {
					return err
				}
				if kind == "rds" {
					items := response["Items"].(map[string]any)["DBInstanceIPArray"].([]any)
					for _, v := range items {
						g := v.(map[string]any)
						if g["DBInstanceIPArrayName"] != "default" {
							g["DBInstanceIPArrayAttribute"] = "hidden"
						}
					}
				} else {
					items := response["SecurityIpGroups"].(map[string]any)["SecurityIpGroup"].([]any)
					for _, v := range items {
						g := v.(map[string]any)
						if g["SecurityIpGroupName"] != "default" {
							g["SecurityIpGroupAttribute"] = "hidden"
						}
					}
				}
				data, _ := json.Marshal(response)
				return json.Unmarshal(data, out)
			}
			writes := f.writes
			if err := c.SyncIP(context.Background(), target, "9.9.9.9", s); err == nil {
				t.Fatal("hidden group adopted")
			}
			if writes != f.writes {
				t.Fatal("mutated changed group")
			}
		})
	}
}
