package aliyun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"yuntools/internal/cloud"
)

var _ cloud.DynamicWhitelist = (*Client)(nil)
var managedIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type managedEntry struct {
	IP     string `json:"ip"`
	RuleID string `json:"ruleId,omitempty"`
	Marker string `json:"marker,omitempty"`
}
type managedPending struct {
	IP           string `json:"ip"`
	Token        string `json:"token,omitempty"`
	Marker       string `json:"marker,omitempty"`
	Creating     bool   `json:"creating,omitempty"`
	Acknowledged bool   `json:"acknowledged,omitempty"`
}
type managedJournal struct {
	Version      int             `json:"version"`
	Scope        string          `json:"scope"`
	Group        string          `json:"group,omitempty"`
	GroupOwned   bool            `json:"groupOwned,omitempty"`
	GroupNetwork string          `json:"groupNetwork,omitempty"`
	Entries      []managedEntry  `json:"entries,omitempty"`
	Pending      *managedPending `json:"pending,omitempty"`
}

// A separate client is constructed by each UI action. Locks must therefore be
// process-wide, covering both the journal and cloud scope, and cancellable.
var managedLocks = struct {
	sync.Mutex
	entries map[string]*managedLock
}{entries: map[string]*managedLock{}}

type managedLock struct {
	token chan struct{}
	refs  int
}

func lockManaged(ctx context.Context, key string) (func(), error) {
	managedLocks.Lock()
	l := managedLocks.entries[key]
	if l == nil {
		l = &managedLock{token: make(chan struct{}, 1)}
		l.token <- struct{}{}
		managedLocks.entries[key] = l
	}
	l.refs++
	managedLocks.Unlock()
	drop := func() {
		managedLocks.Lock()
		l.refs--
		if l.refs == 0 {
			delete(managedLocks.entries, key)
		}
		managedLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-l.token:
		return func() { l.token <- struct{}{}; drop() }, nil
	}
}
func managedScope(t cloud.Target) string {
	data, _ := json.Marshal([]string{t.AccountID, t.Kind, t.Region, t.ID, t.Group, t.Protocol, t.Ports})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func managedGroup(configID, scope string) string {
	sum := sha256.Sum256([]byte(configID + ":" + scope))
	return "yuntools_" + hex.EncodeToString(sum[:11])
}
func managedKey(configID string) string { return "whitelist-managed-v1/" + configID }
func managedMarker(configID, scope, token string) string {
	return "YunTools managed v1:" + configID + ":" + scope + ":" + token
}
func saveManaged(ctx context.Context, store cloud.SecretStore, key string, j *managedJournal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if err = store.Put(key, data); err != nil {
		return fmt.Errorf("cannot persist managed whitelist ownership: %w", err)
	}
	return nil
}

// SyncIP adds and verifies the desired address before removing entries whose
// durable ownership evidence still agrees with the cloud. SourceIP and Automatic
// are deliberately absent from ownership identity. AddIP remains append-only.
func (c *Client) SyncIP(ctx context.Context, t cloud.Target, value string, store cloud.SecretStore) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil {
		return errors.New("managed whitelist requires an ownership store")
	}
	if !managedIDPattern.MatchString(t.ConfigID) {
		return errors.New("persist a valid whitelist configuration ID before syncing")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("a single valid IP address is required")
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("a single valid IP address is required")
	}
	if _, err = c.targetParams(t); err != nil {
		return err
	}
	if t.AccountID == "" {
		t.AccountID = c.account.ID
	}
	if t.Region == "" {
		t.Region = c.account.Region
	}
	if !regionPattern.MatchString(t.Region) {
		return errors.New("invalid region")
	}
	if t.Group == "" {
		t.Group = "default"
	}
	if t.Kind == "ecs" {
		if err = validatePorts(t); err != nil {
			return err
		}
	} else if !ip.Is4() {
		return errors.New("managed RDS/Redis whitelists require an IPv4 address")
	}
	scope := managedScope(t)
	unlock, err := lockManaged(ctx, "config:"+t.ConfigID)
	if err != nil {
		return err
	}
	defer unlock()
	unlockScope, err := lockManaged(ctx, "scope:"+scope)
	if err != nil {
		return err
	}
	defer unlockScope()
	key := managedKey(t.ConfigID)
	j := &managedJournal{Version: 1, Scope: scope}
	data, err := store.Get(key)
	if err == nil {
		j = new(managedJournal)
		if json.Unmarshal(data, j) != nil || j.Version != 1 || j.Scope != scope {
			return errors.New("managed whitelist ownership scope changed or journal is invalid; inspect the existing configuration")
		}
	} else if !errors.Is(err, cloud.ErrNotFound) {
		return fmt.Errorf("cannot read managed whitelist ownership: %w", err)
	}
	if err = c.validateManagedJournal(t, j); err != nil {
		return err
	}
	if t.Kind == "ecs" {
		return c.syncManagedECS(ctx, t, ip, store, key, j)
	}
	return c.syncManagedGroup(ctx, t, ip, store, key, j)
}
func (c *Client) validateManagedJournal(t cloud.Target, j *managedJournal) error {
	invalid := errors.New("managed whitelist ownership journal is invalid; refusing cloud changes")
	if t.Kind != "ecs" && j.Group != "" && j.Group != managedGroup(t.ConfigID, j.Scope) {
		return invalid
	}
	for _, e := range j.Entries {
		ip, err := netip.ParseAddr(e.IP)
		if err != nil || ip.String() != e.IP {
			return invalid
		}
		if t.Kind == "ecs" {
			prefix := managedMarker(t.ConfigID, j.Scope, "")
			if e.RuleID == "" || !strings.HasPrefix(e.Marker, prefix) || !managedIDPattern.MatchString(strings.TrimPrefix(e.Marker, prefix)) {
				return invalid
			}
		} else if !ip.Is4() || !j.GroupOwned || j.Group == "" {
			return invalid
		}
	}
	if p := j.Pending; p != nil {
		ip, err := netip.ParseAddr(p.IP)
		if err != nil || ip.String() != p.IP {
			return invalid
		}
		if t.Kind == "ecs" {
			if !managedIDPattern.MatchString(p.Token) || p.Marker != managedMarker(t.ConfigID, j.Scope, p.Token) {
				return invalid
			}
		} else if !ip.Is4() || j.Group == "" {
			return invalid
		}
	}
	return nil
}
func (c *Client) managedCall(ctx context.Context, t cloud.Target, action string, p map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var result map[string]any
	return c.call(ctx, t.Kind, action, p, &result)
}
func managedRuleMatches(r permission, t cloud.Target, ip netip.Addr) bool {
	return ((ip.Is4() && r.Ipv6SourceCidrIp == "") || (!ip.Is4() && r.SourceCidrIp == "")) && matchesRule(r, t, ip) && r.SourceGroupId == "" && r.DestGroupId == "" && r.SourcePrefixListId == "" && r.DestPrefixListId == "" && r.DestCidrIp == "" && r.Ipv6DestCidrIp == "" && (r.SourcePortRange == "" || r.SourcePortRange == "-1/-1") && (r.NicType == "" || strings.EqualFold(r.NicType, "intranet"))
}
func managedRuleOwned(r permission, t cloud.Target, e managedEntry) bool {
	ip, err := netip.ParseAddr(e.IP)
	return err == nil && r.SecurityGroupRuleId == e.RuleID && r.Description == e.Marker && r.Priority.String() == "1" && managedRuleMatches(r, t, ip)
}
func findManagedRule(rules []permission, id string) (permission, bool) {
	for _, r := range rules {
		if r.SecurityGroupRuleId == id {
			return r, true
		}
	}
	return permission{}, false
}
func managedHasRule(rules []permission, t cloud.Target, ip netip.Addr) bool {
	for _, r := range rules {
		if managedRuleMatches(r, t, ip) {
			return true
		}
	}
	return false
}
func (c *Client) syncManagedECS(ctx context.Context, t cloud.Target, ip netip.Addr, store cloud.SecretStore, key string, j *managedJournal) error {
	rules, err := c.permissions(ctx, t)
	if err != nil {
		return err
	}
	// Forget missing IDs before another write; never infer ownership by address.
	kept := make([]managedEntry, 0, len(j.Entries))
	for _, e := range j.Entries {
		r, ok := findManagedRule(rules, e.RuleID)
		if !ok {
			continue
		}
		if !managedRuleOwned(r, t, e) {
			return errors.New("managed security-group rule was modified; refusing automatic cleanup")
		}
		kept = append(kept, e)
	}
	j.Entries = kept
	if err = saveManaged(ctx, store, key, j); err != nil {
		return err
	}
	if j.Pending != nil {
		rules, err = c.finishManagedECSAdd(ctx, t, store, key, j, rules)
		if err != nil {
			return err
		}
	}
	if !managedHasRule(rules, t, ip) {
		var token [16]byte
		if _, err = rand.Read(token[:]); err != nil {
			return err
		}
		nonce := hex.EncodeToString(token[:])
		j.Pending = &managedPending{IP: ip.String(), Token: nonce, Marker: managedMarker(t.ConfigID, j.Scope, nonce)}
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
		rules, err = c.finishManagedECSAdd(ctx, t, store, key, j, rules)
		if err != nil {
			return err
		}
	}
	if !managedHasRule(rules, t, ip) {
		return errors.New("new managed security-group rule is not visible; old rules retained")
	}
	for i := 0; i < len(j.Entries); {
		old := j.Entries[i]
		if old.IP == ip.String() {
			i++
			continue
		}
		// Re-read immediately before destructive actions, verifying both new access
		// and the exact old rule ID, marker and attributes.
		rules, err = c.permissions(ctx, t)
		if err != nil {
			return err
		}
		if !managedHasRule(rules, t, ip) {
			return errors.New("new rule disappeared; old rules retained")
		}
		r, exists := findManagedRule(rules, old.RuleID)
		if exists {
			if !managedRuleOwned(r, t, old) {
				return errors.New("managed rule ownership changed; old rule retained")
			}
			p, _ := c.targetParams(t)
			p["SecurityGroupRuleId.1"] = old.RuleID
			if err = c.managedCall(ctx, t, "RevokeSecurityGroup", p); err != nil {
				return fmt.Errorf("new IP verified; previous managed rule cleanup failed: %w", err)
			}
			rules, err = c.permissions(ctx, t)
			if err != nil {
				return err
			}
			if _, exists = findManagedRule(rules, old.RuleID); exists {
				return errors.New("old managed rule is still visible; retry cleanup later")
			}
		}
		j.Entries = append(j.Entries[:i], j.Entries[i+1:]...)
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) finishManagedECSAdd(ctx context.Context, t cloud.Target, store cloud.SecretStore, key string, j *managedJournal, rules []permission) ([]permission, error) {
	pending := j.Pending
	ip := netip.MustParseAddr(pending.IP)
	recoverOwned := func() bool {
		for _, r := range rules {
			if r.Description == pending.Marker && r.SecurityGroupRuleId != "" && r.Priority.String() == "1" && managedRuleMatches(r, t, ip) {
				j.Entries = append(j.Entries, managedEntry{IP: pending.IP, RuleID: r.SecurityGroupRuleId, Marker: pending.Marker})
				j.Pending = nil
				return true
			}
		}
		return false
	}
	if recoverOwned() {
		return rules, saveManaged(ctx, store, key, j)
	}
	// A concurrent foreign rule may satisfy access, but is never adopted.
	if managedHasRule(rules, t, ip) {
		j.Pending = nil
		return rules, saveManaged(ctx, store, key, j)
	}
	p, _ := c.targetParams(t)
	p["IpProtocol"] = t.Protocol
	p["PortRange"] = t.Ports
	p["Policy"] = "accept"
	p["Priority"] = "1"
	p["Description"] = pending.Marker
	p["ClientToken"] = pending.Token
	if ip.Is4() {
		p["SourceCidrIp"] = ip.String() + "/32"
	} else {
		p["Ipv6SourceCidrIp"] = ip.String() + "/128"
	}
	if err := c.managedCall(ctx, t, "AuthorizeSecurityGroup", p); err != nil {
		return nil, err
	}
	var err error
	rules, err = c.permissions(ctx, t)
	if err != nil {
		return nil, err
	}
	if recoverOwned() {
		return rules, saveManaged(ctx, store, key, j)
	}
	if managedHasRule(rules, t, ip) {
		j.Pending = nil
		return rules, saveManaged(ctx, store, key, j)
	}
	return nil, errors.New("new managed security-group rule is not visible; old rules retained")
}

type managedIPGroup struct {
	Name, Attribute, Network string
	IPs                      []string
}

func (c *Client) managedGroups(ctx context.Context, t cloud.Target) (map[string]managedIPGroup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, _ := c.targetParams(t)
	groups := map[string]managedIPGroup{}
	add := func(g managedIPGroup) error {
		if _, ok := groups[g.Name]; ok {
			return errors.New("ambiguous duplicate whitelist group name")
		}
		groups[g.Name] = g
		return nil
	}
	if t.Kind == "rds" {
		var r struct {
			Items struct {
				DBInstanceIPArray []struct{ DBInstanceIPArrayName, DBInstanceIPArrayAttribute, WhitelistNetworkType, SecurityIPList string }
			}
		}
		if err := c.call(ctx, t.Kind, "DescribeDBInstanceIPArrayList", p, &r); err != nil {
			return nil, err
		}
		for _, g := range r.Items.DBInstanceIPArray {
			if err := add(managedIPGroup{g.DBInstanceIPArrayName, g.DBInstanceIPArrayAttribute, g.WhitelistNetworkType, splitIPs(g.SecurityIPList)}); err != nil {
				return nil, err
			}
		}
	} else {
		var r struct {
			SecurityIpGroups struct {
				SecurityIpGroup []struct{ SecurityIpGroupName, SecurityIpGroupAttribute, SecurityIpList string }
			}
		}
		if err := c.call(ctx, t.Kind, "DescribeSecurityIps", p, &r); err != nil {
			return nil, err
		}
		for _, g := range r.SecurityIpGroups.SecurityIpGroup {
			if err := add(managedIPGroup{g.SecurityIpGroupName, g.SecurityIpGroupAttribute, "", splitIPs(g.SecurityIpList)}); err != nil {
				return nil, err
			}
		}
	}
	return groups, nil
}
func (c *Client) modifyManagedGroup(ctx context.Context, t cloud.Target, group, mode, ip string) error {
	p, _ := c.targetParams(t)
	p["ModifyMode"] = mode
	p["SecurityIps"] = ip
	if t.Kind == "rds" {
		p["DBInstanceIPArrayName"] = group
	} else {
		p["SecurityIpGroupName"] = group
	}
	return c.managedCall(ctx, t, "ModifySecurityIps", p)
}
func validateManagedGroup(g managedIPGroup, j *managedJournal) error {
	if g.Attribute != "" || (j.GroupOwned && g.Network != j.GroupNetwork) {
		return errors.New("managed whitelist group metadata changed; refusing cloud changes")
	}
	return nil
}
func (c *Client) syncManagedGroup(ctx context.Context, t cloud.Target, ip netip.Addr, store cloud.SecretStore, key string, j *managedJournal) error {
	groups, err := c.managedGroups(ctx, t)
	if err != nil {
		return err
	}
	name := managedGroup(t.ConfigID, j.Scope)
	g, exists := groups[name]
	if j.Group == "" {
		if exists {
			return errors.New("dedicated whitelist group already exists without ownership proof; it will not be adopted")
		}
		j.Group = name
	}
	if exists && !j.GroupOwned && (j.Pending == nil || !j.Pending.Creating || !j.Pending.Acknowledged) {
		return errors.New("dedicated whitelist group has no confirmed ownership; existing group will not be adopted")
	}
	if exists {
		if err = validateManagedGroup(g, j); err != nil {
			return err
		}
	} else if j.GroupOwned {
		return errors.New("managed whitelist group disappeared; refusing to adopt or recreate it")
	}
	// An ambiguous first creation cannot establish ownership. On later appends,
	// unknown results are left unowned, even when the address is observed.
	if pending := j.Pending; pending != nil {
		if pending.Acknowledged {
			if !exists || !hasIP(g.IPs, netip.MustParseAddr(pending.IP)) {
				return errors.New("submitted managed IP is not visible; old entries retained")
			}
			j.Entries = append(j.Entries, managedEntry{IP: pending.IP})
			if pending.Creating {
				j.GroupOwned = true
				j.GroupNetwork = g.Network
			}
			j.Pending = nil
		} else if pending.Creating && exists {
			return errors.New("whitelist group creation outcome is uncertain; existing group will not be adopted")
		} else {
			j.Pending = nil
		}
	}
	// Never restore ownership of an address observed missing on a previous read.
	kept := make([]managedEntry, 0, len(j.Entries))
	for _, e := range j.Entries {
		if hasIP(g.IPs, netip.MustParseAddr(e.IP)) {
			kept = append(kept, e)
		}
	}
	j.Entries = kept
	if err = saveManaged(ctx, store, key, j); err != nil {
		return err
	}
	if !hasIP(g.IPs, ip) {
		j.Pending = &managedPending{IP: ip.String(), Creating: !j.GroupOwned}
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
		if err = c.modifyManagedGroup(ctx, t, name, "Append", ip.String()); err != nil {
			return err
		}
		j.Pending.Acknowledged = true
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
		groups, err = c.managedGroups(ctx, t)
		if err != nil {
			return err
		}
		g, exists = groups[name]
		if !exists || !hasIP(g.IPs, ip) {
			return errors.New("new managed IP is not visible; old entries retained")
		}
		if err = validateManagedGroup(g, j); err != nil {
			return err
		}
		j.GroupOwned = true
		j.GroupNetwork = g.Network
		j.Entries = append(j.Entries, managedEntry{IP: ip.String()})
		j.Pending = nil
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
	}
	for i := 0; i < len(j.Entries); {
		old := j.Entries[i]
		if old.IP == ip.String() {
			i++
			continue
		}
		groups, err = c.managedGroups(ctx, t)
		if err != nil {
			return err
		}
		g, exists = groups[name]
		if !exists || !hasIP(g.IPs, ip) {
			return errors.New("new managed IP disappeared; old entries retained")
		}
		if err = validateManagedGroup(g, j); err != nil {
			return err
		}
		if hasIP(g.IPs, netip.MustParseAddr(old.IP)) {
			if err = c.modifyManagedGroup(ctx, t, name, "Delete", old.IP); err != nil {
				return fmt.Errorf("new IP verified; previous managed IP cleanup failed: %w", err)
			}
			groups, err = c.managedGroups(ctx, t)
			if err != nil {
				return err
			}
			g, exists = groups[name]
			if !exists || !hasIP(g.IPs, ip) {
				return errors.New("managed whitelist changed during cleanup; inspect cloud state")
			}
			if hasIP(g.IPs, netip.MustParseAddr(old.IP)) {
				return errors.New("old managed IP is still visible; retry cleanup later")
			}
		}
		j.Entries = append(j.Entries[:i], j.Entries[i+1:]...)
		if err = saveManaged(ctx, store, key, j); err != nil {
			return err
		}
	}
	return nil
}
