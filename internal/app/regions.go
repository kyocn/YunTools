package app

import (
	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

type regionOption struct{ code, name string }

// Public ECS regions verified 2026-11-06 against Alibaba Cloud's region table:
// https://help.aliyun.com/zh/ecs/user-guide/regions-and-zones
// Individual products may support a subset. Keep unknown saved IDs as options
// so an offline catalog update never silently changes an account's endpoint.
var publicRegions = []regionOption{
	{"cn-beijing", "北京"}, {"cn-shanghai", "上海"}, {"cn-hangzhou", "杭州"},
	{"cn-shenzhen", "深圳"}, {"cn-guangzhou", "广州"}, {"cn-chengdu", "成都"},
	{"cn-qingdao", "青岛"}, {"cn-zhangjiakou", "张家口"}, {"cn-huhehaote", "呼和浩特"},
	{"cn-wulanchabu", "乌兰察布"}, {"cn-wuhan-lr", "武汉（本地地域）"},
	{"cn-heyuan", "河源"}, {"cn-zhongwei", "中卫"}, {"cn-hongkong", "香港"},
	{"ap-southeast-1", "新加坡"}, {"ap-southeast-3", "吉隆坡"},
	{"ap-southeast-5", "雅加达"}, {"ap-southeast-6", "马尼拉"},
	{"ap-southeast-7", "曼谷"}, {"ap-southeast-8", "柔佛州"},
	{"ap-northeast-1", "东京"}, {"ap-northeast-2", "首尔"},
	{"us-west-1", "硅谷"}, {"us-east-1", "弗吉尼亚"},
	{"eu-central-1", "法兰克福"}, {"eu-west-1", "伦敦"}, {"eu-west-2", "巴黎"},
	{"me-east-1", "迪拜"}, {"me-central-1", "利雅得（合作伙伴）"},
	{"na-south-1", "墨西哥"}, {"sa-east-1", "圣保罗"},
}

var tencentRegions = []regionOption{
	{"ap-guangzhou", "广州"}, {"ap-shanghai", "上海"}, {"ap-beijing", "北京"},
	{"ap-chengdu", "成都"}, {"ap-chongqing", "重庆"}, {"ap-nanjing", "南京"},
	{"ap-hongkong", "中国香港"}, {"ap-singapore", "新加坡"}, {"ap-bangkok", "曼谷"},
	{"ap-jakarta", "雅加达"}, {"ap-tokyo", "东京"}, {"ap-seoul", "首尔"},
	{"na-siliconvalley", "硅谷"}, {"na-ashburn", "弗吉尼亚"}, {"na-toronto", "多伦多"},
	{"eu-frankfurt", "法兰克福"}, {"sa-saopaulo", "圣保罗"},
}

var qiniuRegions = []regionOption{
	{"z0", "华东-浙江"},
	{"cn-east-2", "华东-浙江2"},
	{"z1", "华北-河北"},
	{"z2", "华南-广东"},
	{"na0", "北美-洛杉矶"},
	{"as0", "亚太-新加坡"},
	{"ap-northeast-1", "亚太-首尔"},
}

func defaultRegionForProvider(provider string) string {
	switch cloud.NormalizeProvider(provider) {
	case cloud.ProviderTencent:
		return "ap-guangzhou"
	case cloud.ProviderQiniu:
		return "z0"
	default:
		return "cn-hangzhou"
	}
}

func regionsForProvider(provider string) []regionOption {
	switch cloud.NormalizeProvider(provider) {
	case cloud.ProviderTencent:
		return tencentRegions
	case cloud.ProviderQiniu:
		return qiniuRegions
	default:
		return publicRegions
	}
}

func resourceRegionName(region string) string {
	return resourceRegionNameForProvider(cloud.ProviderAliyun, region)
}

func (a *App) regionLabel(region string) string {
	return resourceRegionNameForProvider(a.currentProvider(), region)
}

func resourceRegionNameForProvider(provider, region string) string {
	for _, option := range regionsForProvider(provider) {
		if option.code == region {
			return option.name + "（" + region + "）"
		}
	}
	return region
}

// regionSelect exposes readable names while binding only the original API code.
// Its returned Element supports the same Changed/Disabled/width API as Select.
func regionSelect(c *ui.Context, code *string) ui.Element {
	return regionSelectForProvider(c, cloud.ProviderAliyun, code)
}

func regionSelectForProvider(c *ui.Context, provider string, code *string) ui.Element {
	catalog := regionsForProvider(provider)
	options := make([]string, 0, len(catalog)+1)
	labelsToCodes := make(map[string]string, len(catalog)+1)
	selected := ""
	for _, option := range catalog {
		label := option.name + "（" + option.code + "）"
		options = append(options, label)
		labelsToCodes[label] = option.code
		if option.code == *code {
			selected = label
		}
	}
	if selected == "" {
		selected = "请选择地域"
		if *code != "" {
			selected = "已保存地域（" + *code + "）"
		}
		options = append([]string{selected}, options...)
		labelsToCodes[selected] = *code
	}
	input := ui.Select(c, &selected, options)
	if input.Changed() {
		if value, ok := labelsToCodes[selected]; ok {
			*code = value
		}
	}
	return input
}
