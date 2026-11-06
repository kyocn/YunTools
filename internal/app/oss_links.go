package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

func (a *App) openObjectLink(o cloud.Object) {
	if o.IsDir || o.Key == "" || a.bucket == "" {
		return
	}
	p, err := a.storageAccount()
	if err != nil {
		a.err = a.safeError(err)
		return
	}
	a.err = ""
	a.dialog = &form{Kind: "object-link", Title: "复制对象路径与访问链接", Account: p, Fields: map[string]string{
		"bucket": a.bucket, "source": o.Key, "expires": strconv.Itoa(int(cloud.DefaultObjectLinkExpiry / time.Second)),
	}}
}

func objectLinkFieldSpecs() []fieldSpec {
	return []fieldSpec{
		{key: "bucket", label: "Bucket", readOnly: true},
		{key: "source", label: "对象路径", readOnly: true},
		{key: "expires", label: "签名有效期（秒）", placeholder: "300"},
	}
}

func (a *App) objectLinkFormHelp(c *ui.Context, f *form) {
	p := theme.Colors(a.dark)
	ui.Text(c, "确认公开的对象使用普通 HTTPS 链接；私有、无法确认权限或公共访问受限时使用签名链接。有效期默认 300 秒，可设为 1–604800 秒；临时凭据到期可能使链接提前失效。").FillWidth().FontSize(11).TextColor(p.Muted)
	ui.Text(c, "访问仍受账号权限和 Bucket 策略限制。OSS 路径仅用于定位对象。").FillWidth().FontSize(11).TextColor(p.Muted)
	if ui.Button(c, "复制 OSS 路径").Radius(999).Disabled(a.busy > 0).Clicked() {
		a.copyObjectPath(*f)
	}
	if a.err != "" {
		ui.Text(c, a.err).FillWidth().FontSize(12).TextColor(p.Red).Selectable()
	}
}

func objectLinkFormExpiry(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return cloud.DefaultObjectLinkExpiry, nil
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || seconds < 1 || seconds > int64(cloud.MaxObjectLinkExpiry/time.Second) {
		return 0, errors.New("链接有效期必须为 1–604800 秒的整数")
	}
	return time.Duration(seconds) * time.Second, nil
}

func (a *App) validObjectLinkForm(f form) bool {
	if f.Account.ID != a.accountID || f.Fields["bucket"] != a.bucket || f.Fields["source"] == "" {
		a.err = "对象所在账号或 Bucket 已切换，请重新打开复制链接"
		return false
	}
	if a.p.CopyText == nil {
		a.err = "系统剪贴板不可用，请重试"
		return false
	}
	return true
}

func (a *App) copyObjectPath(f form) {
	if !a.validObjectLinkForm(f) {
		return
	}
	if err := a.p.CopyText("oss://" + f.Fields["bucket"] + "/" + f.Fields["source"]); err != nil {
		a.err = "无法写入系统剪贴板，请重试"
		return
	}
	a.err = ""
	a.notice = "OSS 路径已复制"
}

func (a *App) copyObjectLink(f form) {
	if !a.validObjectLinkForm(f) {
		return
	}
	expiry, err := objectLinkFormExpiry(f.Fields["expires"])
	if err != nil {
		a.err = err.Error()
		return
	}
	bucket, key := f.Fields["bucket"], f.Fields["source"]
	dialog := a.dialog
	a.async(func(ctx context.Context) (func(), error) {
		storage, err := a.p.Storage(f.Account)
		if err != nil {
			return nil, err
		}
		linker, ok := storage.(cloud.ObjectLinker)
		if !ok {
			return nil, errors.New("当前对象存储暂不支持生成访问链接")
		}
		link, err := linker.ObjectLink(ctx, bucket, key, expiry)
		if err != nil {
			return nil, err
		}
		if link.URL == "" {
			return nil, errors.New("未能生成访问链接，请重试")
		}
		return func() {
			// dispatch also checks the view generation before this clipboard side effect.
			if a.dialog != dialog || a.bucket != bucket || a.accountID != f.Account.ID {
				return
			}
			if err := a.p.CopyText(link.URL); err != nil {
				a.err = "无法写入系统剪贴板，请重试"
				return
			}
			a.dialog = nil
			a.notice = "公开访问链接已复制（无签名）"
			if link.Signed {
				a.notice = fmt.Sprintf("签名访问链接已复制，有效期 %d 秒", int(expiry/time.Second))
				if !link.ExpiresAt.IsZero() {
					a.notice += "，到期时间 " + link.ExpiresAt.In(beijingTime).Format("15:04:05") + "（北京时间）"
				}
				if link.Reason != "" {
					a.notice += "。" + link.Reason
				}
			}
		}, nil
	})
}
