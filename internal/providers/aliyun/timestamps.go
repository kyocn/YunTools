package aliyun

import "time"

// ECS and RDS return ISO 8601 UTC timestamps. ECS documents minute precision
// but its examples (and Redis) include seconds. Never assume a local timezone.
// https://help.aliyun.com/en/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroups
// https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstances
func parseCloudTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// AliDNS CreateTimestamp and UpdateTimestamp use Unix milliseconds.
// https://help.aliyun.com/en/dns/api-alidns-2015-01-09-describedomainrecords
func parseCloudMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
