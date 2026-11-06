package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"yuntools/internal/cloud"
)

func fixture(out any, s string) error { return json.Unmarshal([]byte(s), out) }
func TestWhitelistAppendPreservesExistingAndDeduplicates(t *testing.T) {
	calls := 0
	ips := "10.1.1.1"
	c := &Client{account: cloud.Account{ID: "a", Region: "cn-hangzhou"}, call: func(ctx context.Context, p, a string, q map[string]string, out any) error {
		if a == "DescribeDBInstanceIPArrayList" {
			return fixture(out, fmt.Sprintf(`{"Items":{"DBInstanceIPArray":[{"DBInstanceIPArrayName":"default","SecurityIPList":%q}]}}`, ips))
		}
		if a != "ModifySecurityIps" || q["ModifyMode"] != "Append" || q["SecurityIps"] != "8.8.8.8" {
			t.Fatalf("unsafe mutation %s %#v", a, q)
		}
		calls++
		ips += ",8.8.8.8"
		return fixture(out, `{}`)
	}}
	target := cloud.Target{AccountID: "a", Kind: "rds", ID: "db", Group: "default"}
	for range 2 {
		if err := c.AddIP(context.Background(), target, "8.8.8.8"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || ips != "10.1.1.1,8.8.8.8" {
		t.Fatalf("calls=%d ips=%s", calls, ips)
	}
}
func TestECSRequiresExplicitProtocolPorts(t *testing.T) {
	c := &Client{account: cloud.Account{ID: "a"}, call: func(context.Context, string, string, map[string]string, any) error {
		t.Fatal("API called on invalid input")
		return nil
	}}
	for _, target := range []cloud.Target{{Kind: "ecs", ID: "sg"}, {Kind: "ecs", ID: "sg", Protocol: "tcp", Ports: "0/65536"}, {Kind: "ecs", ID: "sg", Protocol: "all", Ports: "1/65535"}} {
		if c.AddIP(context.Background(), target, "8.8.8.8") == nil {
			t.Fatal("unsafe rule accepted")
		}
	}
}
func TestChallengeDeleteChecksExactRecord(t *testing.T) {
	deleted := false
	c := &Client{call: func(ctx context.Context, p, a string, q map[string]string, out any) error {
		if a == "DeleteDomainRecord" {
			deleted = true
			return fixture(out, `{}`)
		}
		return fixture(out, `{"RecordId":"r1","DomainName":"example.com","RR":"_acme-challenge","Type":"TXT","Value":"someone-else"}`)
	}}
	err := c.DeleteChallengeRecord(context.Background(), cloud.Record{ID: "r1", Domain: "example.com", RR: "_acme-challenge", Type: "TXT", Value: "ours"})
	if err == nil || deleted {
		t.Fatal("deleted a changed record")
	}
}
func TestListDomainsUsesAllPages(t *testing.T) {
	c := &Client{call: func(ctx context.Context, p, a string, q map[string]string, out any) error {
		return fixture(out, fmt.Sprintf(`{"TotalCount":2,"Domains":{"Domain":[{"DomainId":%q,"DomainName":%q}]}}`, q["PageNumber"], q["PageNumber"]+".com"))
	}}
	ds, err := c.ListDomains(context.Background())
	if err != nil || len(ds) != 2 {
		t.Fatalf("%v %v", ds, err)
	}
}

func TestFreshChallengeNeverAdoptsExistingRecord(t *testing.T) {
	c := &Client{call: func(ctx context.Context, p, a string, q map[string]string, out any) error {
		if a != "DescribeDomainRecords" {
			t.Fatal("created duplicate/adopted existing TXT")
		}
		return fixture(out, `{"TotalCount":1,"DomainRecords":{"Record":[{"RecordId":"foreign","RR":"_acme-challenge","Type":"TXT","Value":"v"}]}}`)
	}}
	_, err := c.CreateChallengeRecord(context.Background(), cloud.Record{Domain: "example.com", RR: "_acme-challenge", Type: "TXT", Value: "v"})
	if err == nil {
		t.Fatal("existing record adopted")
	}
}
func TestNoGeneralDeleteOrUpdateDNSCapability(t *testing.T) {
	typ := reflect.TypeOf((*cloud.DNS)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if strings.Contains(name, "Delete") || strings.Contains(name, "Update") || strings.Contains(name, "Disable") {
			t.Fatal(name)
		}
	}
}
func TestMissingRecordClassifiedForCrashRecovery(t *testing.T) {
	c := &Client{account: cloud.Account{ID: "a", Region: "cn-hangzhou", AccessKeyID: "test", AccessKeySecret: "test"}, transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Code":"InvalidRecordId.NotFound","Message":"missing","RequestId":"test"}`)), Request: r}, nil
	})}
	c.call = c.rpc
	_, err := c.GetRecord(context.Background(), "missing")
	if !errors.Is(err, cloud.ErrNotFound) {
		t.Fatalf("notfound misclassified: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRPCSignsHTTPSAndDoesNotRetryMutation(t *testing.T) {
	calls := 0
	c := &Client{account: cloud.Account{ID: "a", Region: "cn-hangzhou", AccessKeyID: "access-test", AccessKeySecret: "secret-test"}, transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.Method != "POST" {
			t.Fatalf("unsafe request %s %s", r.Method, r.URL.Scheme)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("Signature") == "" || r.Form.Get("AccessKeyId") != "access-test" {
			t.Fatal("unsigned RPC")
		}
		return nil, errors.New("network timeout secret-test")
	})}
	var result map[string]any
	err := c.rpc(context.Background(), "dns", "AddDomainRecord", map[string]string{"DomainName": "example.com"}, &result)
	if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-test") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestForbiddenNamesRequiredRAMActionWithoutLeakingResponse(t *testing.T) {
	for _, tc := range []struct{ product, action, want string }{
		{"ecs", "DescribeSecurityGroupAttribute", "ecs:DescribeSecurityGroupAttribute"},
		{"redis", "DescribeSecurityIps", "kvstore:DescribeSecurityIps"},
		{"rds", "DescribeDBInstances", "rds:DescribeDBInstances"},
	} {
		c := &Client{account: cloud.Account{ID: "a", Region: "cn-beijing", AccessKeyID: "fixture", AccessKeySecret: "secret"}, transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"Code":"Forbidden.RAM","Message":"sensitive response","RequestId":"fixture"}`)), Request: r}, nil
		})}
		var out map[string]any
		err := c.rpc(context.Background(), tc.product, tc.action, nil, &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "sensitive response") {
			t.Fatalf("missing safe permission guidance: %v", err)
		}
	}
}
func TestPublicIPRejectsUntrustedResponses(t *testing.T) {
	for _, body := range []string{"127.0.0.1", "10.0.0.1", "<html>error</html>", "8.8.8.8\nextra"} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		if _, err := fetchPublicIP(context.Background(), client, "https://example.test"); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestRPCUsesDocumentedPublicEndpoints(t *testing.T) {
	cases := []struct{ product, region, host string }{
		{"redis", "cn-hangzhou", "r-kvstore.aliyuncs.com"},
		{"redis", "cn-hangzhou-finance", "r-kvstore.aliyuncs.com"},
		{"redis", "cn-beijing", "r-kvstore.cn-beijing.aliyuncs.com"},
		{"redis", "cn-shanghai-finance-1", "r-kvstore.cn-shanghai-finance-1.aliyuncs.com"},
		{"rds", "cn-hangzhou", "rds.aliyuncs.com"},
		{"rds", "cn-hangzhou-finance", "rds.aliyuncs.com"},
		{"rds", "cn-north-2-gov-1", "rds.cn-north-2-gov-1.aliyuncs.com"},
		{"rds", "ap-southeast-8", "rds.ap-southeast-8.aliyuncs.com"},
		{"ecs", "cn-hangzhou", "ecs.cn-hangzhou.aliyuncs.com"},
		{"ecs", "cn-hangzhou-finance", "ecs.aliyuncs.com"},
		{"ecs", "cn-north-2-gov-1", "ecs.aliyuncs.com"},
		{"ecs", "cn-shenzhen-finance-1", "ecs.cn-shenzhen-finance-1.aliyuncs.com"},
	}
	for _, tc := range cases {
		t.Run(tc.product+"/"+tc.region, func(t *testing.T) {
			c := &Client{account: cloud.Account{ID: "a", Region: "cn-hangzhou", AccessKeyID: "test", AccessKeySecret: "test"}, transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != tc.host {
					t.Errorf("endpoint=%s want=%s", r.URL.Host, tc.host)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if got := r.Form.Get("RegionId"); got != tc.region {
					t.Errorf("region=%s want=%s", got, tc.region)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			})}
			var out map[string]any
			if err := c.rpc(context.Background(), tc.product, "DescribeRegions", map[string]string{"RegionId": tc.region}, &out); err != nil {
				t.Fatal(err)
			}
		})
	}
}
