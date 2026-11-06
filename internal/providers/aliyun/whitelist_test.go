package aliyun

import (
	"context"
	"fmt"
	"testing"
	"yuntools/internal/cloud"
)

// ECS returns title-case Policy and upper-case IpProtocol in real responses.
func TestECSTitleCaseRulesVerifyAndAvoidDuplicateWrites(t *testing.T) {
	for _, alreadyExists := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing=%t", alreadyExists), func(t *testing.T) {
			present, writes := alreadyExists, 0
			c := &Client{account: cloud.Account{ID: "a", Region: "cn-shanghai"}}
			c.call = func(ctx context.Context, product, action string, params map[string]string, out any) error {
				switch action {
				case "DescribeSecurityGroupAttribute":
					// Same source alone is insufficient: deny, wrong protocol/port or egress must not count.
					rules := `{"Policy":"Drop","IpProtocol":"TCP","PortRange":"22/22","SourceCidrIp":"8.8.8.8/32"},
					{"Policy":"Accept","IpProtocol":"UDP","PortRange":"22/22","SourceCidrIp":"8.8.8.8/32"},
					{"Policy":"Accept","IpProtocol":"TCP","PortRange":"80/80","SourceCidrIp":"8.8.8.8/32"},
					{"Policy":"Accept","IpProtocol":"TCP","PortRange":"22/22","Direction":"Egress","SourceCidrIp":"8.8.8.8/32"}`
					if present {
						rules += `,{"Policy":"Accept","IpProtocol":"TCP","PortRange":"22/22","Direction":"Ingress","SourceCidrIp":"8.8.8.8/32"}`
					}
					return fixture(out, `{"Permissions":{"Permission":[`+rules+`]}}`)
				case "AuthorizeSecurityGroup":
					writes++
					if params["IpProtocol"] != "tcp" || params["Policy"] != "accept" || params["PortRange"] != "22/22" || params["SourceCidrIp"] != "8.8.8.8/32" {
						t.Fatalf("unexpected write: %v", params)
					}
					present = true
					return fixture(out, `{}`)
				default:
					t.Fatalf("unexpected API: %s", action)
				}
				return nil
			}
			target := cloud.Target{AccountID: "a", Kind: "ecs", ID: "sg-test", Region: "cn-shanghai", Protocol: "tcp", Ports: "22/22"}
			for range 2 {
				if err := c.AddIP(context.Background(), target, "8.8.8.8"); err != nil {
					t.Fatal(err)
				}
			}
			want := 1
			if alreadyExists {
				want = 0
			}
			if writes != want {
				t.Fatalf("writes=%d, want=%d", writes, want)
			}
		})
	}
}

func TestSecurityGroupDetailsIncludeBothDirectionsAndAllPages(t *testing.T) {
	calls := 0
	c := &Client{account: cloud.Account{ID: "a", Region: "cn-shanghai"}}
	c.call = func(ctx context.Context, product, action string, params map[string]string, out any) error {
		calls++
		if action != "DescribeSecurityGroupAttribute" || params["Direction"] != "all" {
			t.Fatalf("not a complete read-only query: %s %v", action, params)
		}
		if calls == 1 {
			return fixture(out, `{"NextToken":"page2","Permissions":{"Permission":[{"SecurityGroupRuleId":"r1","Direction":"ingress","Policy":"Accept","IpProtocol":"TCP","PortRange":"22/22","SourceCidrIp":"8.8.8.8/32","Priority":1,"Description":"SSH access"}]}}`)
		}
		if params["NextToken"] != "page2" || calls > 2 {
			t.Fatal("bad pagination")
		}
		return fixture(out, `{"Permissions":{"Permission":[{"SecurityGroupRuleId":"r2","Direction":"egress","Policy":"Drop","IpProtocol":"ALL","PortRange":"-1/-1","DestCidrIp":"0.0.0.0/0","Priority":"100","Description":"Outbound rule"}]}}`)
	}
	rules, err := c.ListSecurityGroupRules(context.Background(), cloud.Target{AccountID: "a", Kind: "ecs", ID: "sg-test", Region: "cn-shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].Priority != "1" || rules[0].Description != "SSH access" || rules[0].Source != "8.8.8.8/32" || rules[1].Direction != "egress" || rules[1].Destination != "0.0.0.0/0" || rules[1].Priority != "100" {
		t.Fatalf("incomplete details: %+v", rules)
	}
}
