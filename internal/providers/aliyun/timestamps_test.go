package aliyun

import (
	"context"
	"fmt"
	"testing"
	"time"

	"yuntools/internal/cloud"
)

func TestCloudTimestampFormats(t *testing.T) {
	for _, value := range []string{"2026-11-06T12:34:00Z", "2026-11-06T12:34Z", "2026-11-06T12:34:00.000Z"} {
		if got := parseCloudTime(value); !got.Equal(time.Date(2026, 11, 6, 12, 34, 0, 0, time.UTC)) {
			t.Errorf("%s: %v", value, got)
		}
	}
	for _, value := range []string{"", "broken", "2026-11-06 12:34:00"} {
		if !parseCloudTime(value).IsZero() {
			t.Errorf("invented time for %q", value)
		}
	}
	if got := parseCloudMillis(1666501957000); !got.Equal(time.Date(2022, 10, 23, 5, 12, 37, 0, time.UTC)) {
		t.Fatal(got)
	}
	if !parseCloudMillis(0).IsZero() || !parseCloudMillis(-1).IsZero() {
		t.Fatal("missing timestamps must remain unknown")
	}
}

func TestRulesSortedAfterAllRemotePages(t *testing.T) {
	c := &Client{account: cloud.Account{ID: "a"}}
	c.call = func(_ context.Context, _, action string, q map[string]string, out any) error {
		if action != "DescribeSecurityGroupAttribute" {
			t.Fatalf("unexpected action %s", action)
		}
		if q["NextToken"] == "" {
			return fixture(out, `{"NextToken":"next","Permissions":{"Permission":[{"SecurityGroupRuleId":"unknown"},{"SecurityGroupRuleId":"old","CreateTime":"2026-11-05T00:00:00Z"}]}}`)
		}
		return fixture(out, `{"Permissions":{"Permission":[{"SecurityGroupRuleId":"new","CreateTime":"2026-11-06T00:00:00Z"},{"SecurityGroupRuleId":"bad","CreateTime":"bad"}]}}`)
	}
	rules, err := c.ListSecurityGroupRules(context.Background(), cloud.Target{AccountID: "a", Kind: "ecs", ID: "sg-test", Region: "cn-shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"new", "old", "unknown", "bad"} {
		if rules[i].ID != id {
			t.Fatalf("order: %+v", rules)
		}
	}
}

func TestTargetCreationTimeFixtures(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{"rds", `"Items":{"DBInstance":[{"DBInstanceId":"unknown"},{"DBInstanceId":"old","CreateTime":"2026-11-05T00:00:00Z"},{"DBInstanceId":"new","CreateTime":"2026-11-06T00:00:00Z"}]}`},
		{"redis", `"Instances":{"KVStoreInstance":[{"InstanceId":"unknown"},{"InstanceId":"old","CreateTime":"2026-11-05T00:00:00Z"},{"InstanceId":"new","CreateTime":"2026-11-06T00:00:00Z"}]}`},
		{"ecs", `"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"unknown"},{"SecurityGroupId":"old","CreationTime":"2026-11-05T00:00Z"},{"SecurityGroupId":"new","CreationTime":"2026-11-06T00:00:00Z"}]}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c := &Client{account: cloud.Account{ID: "a", Region: "cn-shanghai"}}
			c.call = func(_ context.Context, _, _ string, _ map[string]string, out any) error {
				return fixture(out, `{"TotalCount":3,`+tc.body+`}`)
			}
			got, err := c.ListTargets(context.Background(), tc.kind, "")
			if err != nil || len(got) != 3 {
				t.Fatalf("%v %v", got, err)
			}
			for i, id := range []string{"new", "old", "unknown"} {
				if got[i].ID != id {
					t.Fatalf("order: %+v", got)
				}
			}
			if got[0].Created.Hour() != 0 || got[0].Created.Location() != time.UTC {
				t.Fatal(got[0].Created)
			}
		})
	}
}

func TestDNSCreationAndUpdateFixturesAcrossPages(t *testing.T) {
	c := &Client{}
	c.call = func(_ context.Context, _, action string, q map[string]string, out any) error {
		var values string
		if q["PageNumber"] == "1" {
			values = `{"RecordId":"old","DomainId":"old","CreateTimestamp":1666501957000}`
		} else {
			values = `{"RecordId":"new","DomainId":"new","CreateTimestamp":1676872961000,"UpdateTimestamp":1676872962000},{"RecordId":"unknown","DomainId":"unknown"}`
		}
		if action == "DescribeDomains" {
			return fixture(out, fmt.Sprintf(`{"TotalCount":3,"Domains":{"Domain":[%s]}}`, values))
		}
		return fixture(out, fmt.Sprintf(`{"TotalCount":3,"DomainRecords":{"Record":[%s]}}`, values))
	}
	domains, err := c.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := c.ListRecords(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"new", "old", "unknown"} {
		if domains[i].ID != id || records[i].ID != id {
			t.Fatalf("order: %v %v", domains, records)
		}
	}
	if records[0].Updated.Sub(records[0].Created) != time.Second {
		t.Fatal("update timestamp was not decoded independently")
	}
}
