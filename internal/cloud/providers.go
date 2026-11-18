package cloud

import (
	"errors"
	"strings"
)

const (
	ProviderAliyun  = "aliyun"
	ProviderTencent = "tencent"
	ProviderQiniu   = "qiniu"
)

var ErrUnsupportedProviderCapability = errors.New("当前云厂商不支持此功能")

func NormalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return ProviderAliyun
	}
	return p
}

func SupportedProvider(provider string) bool {
	switch NormalizeProvider(provider) {
	case ProviderAliyun, ProviderTencent, ProviderQiniu:
		return true
	default:
		return false
	}
}

func ProviderDisplayName(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderTencent:
		return "腾讯云"
	case ProviderQiniu:
		return "七牛云"
	default:
		return "阿里云"
	}
}

func ProviderBadge(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderTencent:
		return "TENCENT"
	case ProviderQiniu:
		return "QINIU"
	default:
		return "ALIYUN"
	}
}

func ProviderStorageScheme(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderTencent:
		return "cos"
	case ProviderQiniu:
		return "kodo"
	default:
		return "oss"
	}
}

func ProviderSupportsWhitelist(provider string) bool {
	switch NormalizeProvider(provider) {
	case ProviderAliyun, ProviderTencent:
		return true
	default:
		return false
	}
}

func ProviderSupportsDNS(provider string) bool {
	switch NormalizeProvider(provider) {
	case ProviderAliyun, ProviderTencent:
		return true
	default:
		return false
	}
}

func ProviderSupportsCertificates(provider string) bool {
	return ProviderSupportsDNS(provider)
}
