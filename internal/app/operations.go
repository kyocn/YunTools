package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"yuntools/internal/certificates"
	"yuntools/internal/cloud"
	"yuntools/internal/providers/aliyun"
	"yuntools/internal/storage"
)

func (a *App) refreshOSS(more bool) {
	p, e := a.storageAccount()
	if e != nil {
		return
	}
	a.prepareOSSPager()
	if a.bucket != "" {
		if more {
			if a.objectPager.next != "" {
				a.loadOSSObjectPage(a.objectPager.page+1, a.objectPager.next)
			}
		} else {
			a.objectPager.reset(a.ossScope())
			a.objects = nil
			a.nextToken = ""
			a.loadOSSObjectPage(0, "")
		}
		return
	}
	a.selected = map[string]bool{}
	a.nextToken = ""
	a.objectPager.request++
	request := a.objectPager.request
	scope := a.ossScope()
	a.async(func(ctx context.Context) (func(), error) {
		client, e := a.p.Storage(p)
		if e != nil {
			return nil, e
		}
		bs, e := client.ListBuckets(ctx)
		return func() {
			if a.objectPager.request != request || a.ossScope() != scope {
				return
			}
			a.buckets = bs
		}, e
	})
}
func (a *App) refreshDNS() {
	p, e := a.current()
	if e != nil {
		return
	}
	domain := a.domain
	a.async(func(ctx context.Context) (func(), error) {
		d, e := a.p.DNS(p)
		if e != nil {
			return nil, e
		}
		if domain == "" {
			domains, e := d.ListDomains(ctx)
			return func() { a.domains = domains }, e
		}
		rs, e := d.ListRecords(ctx, domain)
		return func() { a.records = rs }, e
	})
}
func (a *App) refreshTargets() {
	a.region = strings.TrimSpace(a.region)
	a.resetTargetDiscovery()
	p, e := a.current()
	if e != nil {
		a.targetDiscovery.failed = true
		a.targetDiscovery.message = "请先添加并选择云账号，再加载资源。"
		return
	}
	kind, region := a.resourceKind, a.region
	if region == "" {
		a.targetDiscovery.failed = true
		a.targetDiscovery.message = "请填写资源所在地域，例如上海 cn-shanghai、北京 cn-beijing。"
		return
	}
	scope, request := a.targetDiscovery.scope, a.targetDiscovery.request
	label := resourceRegionName(region) + " · " + resourceKindName(kind)
	a.targetDiscovery.loading = true
	a.targetDiscovery.message = "正在加载 " + label + "，请稍候…"
	a.asyncGlobal(func(ctx context.Context) (func(), error) {
		w, e := a.p.Whitelist(p)
		var ts []cloud.Target
		if e == nil {
			ts, e = w.ListTargets(ctx, kind, region)
		}
		return func() {
			if a.targetDiscovery.request != request || a.targetDiscoveryScope() != scope {
				return
			}
			a.targetDiscovery.loading = false
			if e != nil {
				a.targetDiscovery.failed = true
				a.targetDiscovery.message = label + "：加载失败。请检查网络、账号权限与地域后重试。\n" + a.safeError(e)
				return
			}
			a.targets = ts
			if len(ts) == 0 {
				a.targetDiscovery.message = label + "：未找到资源。请核对地域和账号；已配置目标不会受影响。"
			} else {
				a.targetDiscovery.message = fmt.Sprintf("%s：已加载 %d 项资源。", label, len(ts))
			}
		}, nil
	})
}
func (a *App) refreshCertificates() {
	if a.certs == nil {
		return
	}
	id := a.accountID
	a.async(func(ctx context.Context) (func(), error) {
		cs, e := a.certs.List(id)
		return func() { a.certList = cs }, e
	})
}
func (a *App) selectedKeys() []string {
	var keys []string
	for k, on := range a.selected {
		if on {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
func (a *App) requestDelete() {
	keys := a.selectedKeys()
	if len(keys) == 0 {
		return
	}
	p, e := a.storageAccount()
	if e != nil {
		a.err = a.safeError(e)
		return
	}
	bucket := a.bucket
	// Expand only directory entries, before confirmation, to freeze the exact manifest.
	dirs := map[string]bool{}
	for _, o := range a.objects {
		if o.IsDir && a.selected[o.Key] {
			dirs[o.Key] = true
		}
	}
	if len(dirs) > 0 {
		a.async(func(ctx context.Context) (func(), error) {
			c, e := a.p.Storage(p)
			if e != nil {
				return nil, e
			}
			set := map[string]bool{}
			for _, k := range keys {
				if !dirs[k] {
					set[k] = true
				}
			}
			var walk func(string) error
			walk = func(prefix string) error {
				token := ""
				for {
					page, e := c.ListObjects(ctx, bucket, prefix, token)
					if e != nil {
						return e
					}
					for _, o := range page.Objects {
						if o.IsDir && o.Key != prefix {
							if e = walk(o.Key); e != nil {
								return e
							}
						} else {
							set[o.Key] = true
						}
					}
					if page.NextToken == "" {
						break
					}
					token = page.NextToken
				}
				return nil
			}
			for d := range dirs {
				if e = walk(d); e != nil {
					return nil, e
				}
				set[d] = true
			}
			all := make([]string, 0, len(set))
			for k := range set {
				all = append(all, k)
			}
			sort.Strings(all)
			return func() { a.confirmDelete(p, bucket, all) }, nil
		})
		return
	}
	a.confirmDelete(p, bucket, keys)
}
func (a *App) confirmDelete(p cloud.Account, bucket string, keys []string) {
	manifest, e := storage.NewDeleteManifest(p.ID, bucket, keys)
	if e != nil {
		a.err = a.safeError(e)
		return
	}
	detail := fmt.Sprintf("账号：%s\nBucket：%s\n将删除 %d 个对象（不会清空历史版本）。\n\n%s", p.Name, bucket, len(keys), strings.Join(keys, "\n"))
	a.confirm = &confirmation{Title: "确认删除对象", Detail: detail, Run: func() {
		a.async(func(ctx context.Context) (func(), error) {
			client, e := a.p.Storage(p)
			if e == nil {
				e = storage.ConfirmDelete(ctx, client, manifest, p.ID, bucket, manifest.Keys())
			}
			return func() { a.notice = "删除完成"; a.refreshOSS(false) }, e
		})
	}}
}
func (a *App) deleteBucket(name string) {
	p, e := a.current()
	if e != nil {
		return
	}
	for _, b := range a.buckets {
		if b.Name == name && b.Region != "" {
			p.Region = strings.TrimPrefix(b.Region, "oss-")
		}
	}
	a.confirm = &confirmation{Title: "确认删除 Bucket", Detail: fmt.Sprintf("账号：%s\nBucket：%s\n仅可删除空 Bucket，不会自动清空其中对象。", p.Name, name), Run: func() {
		a.async(func(ctx context.Context) (func(), error) {
			c, e := a.p.Storage(p)
			if e == nil {
				e = c.DeleteBucket(ctx, name)
			}
			return func() { a.bucket = ""; a.notice = "Bucket 已删除"; a.refreshOSS(false) }, e
		})
	}}
}

func (a *App) pickUploads(folder bool) {
	p, e := a.storageAccount()
	if e != nil {
		a.err = a.safeError(e)
		return
	}
	bucket, prefix := a.bucket, a.prefix
	if bucket == "" {
		return
	}
	a.async(func(ctx context.Context) (func(), error) {
		var files []string
		var base string
		if folder {
			if a.p.PickFolder == nil {
				return nil, errors.New("文件夹选择不可用")
			}
			dir, e := a.p.PickFolder()
			if e != nil || dir == "" {
				return nil, e
			}
			base = filepath.Dir(dir)
			e = filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if d.Type()&os.ModeSymlink != 0 {
					return errors.New("文件夹上传不跟随符号链接")
				}
				if !d.IsDir() {
					files = append(files, p)
				}
				return nil
			})
			if e != nil {
				return nil, e
			}
		} else {
			if a.p.PickFiles == nil {
				return nil, errors.New("文件选择不可用")
			}
			files, e = a.p.PickFiles()
			if e != nil {
				return nil, e
			}
		}
		var trs []cloud.Transfer
		for _, file := range files {
			info, e := os.Stat(file)
			if e != nil {
				return nil, e
			}
			if !info.Mode().IsRegular() {
				return nil, errors.New("请选择普通文件")
			}
			key := filepath.Base(file)
			if base != "" {
				key, e = filepath.Rel(base, file)
				if e != nil {
					return nil, e
				}
			}
			trs = append(trs, cloud.Transfer{Bucket: bucket, Key: prefix + filepath.ToSlash(key), LocalPath: file, Size: info.Size(), Modified: info.ModTime()})
		}
		return func() {
			if len(trs) == 0 {
				return
			}
			a.confirm = &confirmation{Title: "确认上传", Detail: fmt.Sprintf("账号：%s\n目标：oss://%s/%s\n上传 %d 个文件；同名对象会被覆盖。", p.Name, bucket, prefix, len(trs)), Run: func() {
				for _, tr := range trs {
					a.submitTransfer("upload", p, tr)
				}
			}}
		}, nil
	})
}
func (a *App) downloadSelected() {
	p, e := a.storageAccount()
	if e != nil {
		return
	}
	keys := a.selectedKeys()
	if len(keys) == 0 {
		return
	}
	bucket, prefix := a.bucket, a.prefix
	selected := map[string]cloud.Object{}
	for _, o := range a.objects {
		if a.selected[o.Key] {
			selected[o.Key] = o
		}
	}
	a.async(func(ctx context.Context) (func(), error) {
		if a.p.PickFolder == nil {
			return nil, errors.New("文件夹选择不可用")
		}
		root, e := a.p.PickFolder()
		if e != nil || root == "" {
			return nil, e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, e
		}
		c, e := a.p.Storage(p)
		if e != nil {
			return nil, e
		}
		var objs []cloud.Object
		var walk func(string) error
		walk = func(pre string) error {
			token := ""
			for {
				page, e := c.ListObjects(ctx, bucket, pre, token)
				if e != nil {
					return e
				}
				for _, o := range page.Objects {
					if o.IsDir {
						if o.Key != pre {
							if e = walk(o.Key); e != nil {
								return e
							}
						}
					} else {
						objs = append(objs, o)
					}
				}
				if page.NextToken == "" {
					return nil
				}
				token = page.NextToken
			}
		}
		for _, key := range keys {
			if selected[key].IsDir {
				if e = walk(key); e != nil {
					return nil, e
				}
			} else {
				o, e := c.Stat(ctx, bucket, key)
				if e != nil {
					return nil, e
				}
				objs = append(objs, o)
			}
		}
		var trs []cloud.Transfer
		seen := map[string]bool{}
		for _, o := range objs {
			if seen[o.Key] {
				continue
			}
			seen[o.Key] = true
			local, e := storage.ResolveDestination(root, strings.TrimPrefix(o.Key, prefix))
			if e != nil {
				return nil, e
			}
			trs = append(trs, cloud.Transfer{Bucket: bucket, Key: o.Key, LocalPath: local, DestinationRoot: root, ETag: o.ETag, VersionID: o.VersionID, Size: o.Size, Modified: o.Modified})
		}
		return func() {
			a.confirm = &confirmation{Title: "确认下载", Detail: fmt.Sprintf("账号：%s\n下载 %d 个文件到：\n%s\n同名本地文件会在下载校验成功后替换。", p.Name, len(trs), root), Run: func() {
				for _, tr := range trs {
					a.submitTransfer("download", p, tr)
				}
			}}
		}, nil
	})
}
func (a *App) newForm(kind, title string, fields map[string]string) {
	if kind == "whitelist" {
		a.err = ""
	}
	p, _ := a.storageAccount()
	a.dialog = &form{Kind: kind, Title: title, Fields: fields, Account: p}
}
func (a *App) saveForm() {
	if a.dialog == nil {
		return
	}
	f := *a.dialog
	p := f.Account
	v := make(map[string]string, len(f.Fields))
	for k, value := range f.Fields {
		v[k] = value
	}
	bucket, domain := a.bucket, a.domain
	switch f.Kind {
	case "account":
		p.Name = v["name"]
		p.Provider = "aliyun"
		p.Region = v["region"]
		p.AccessKeyID = v["ak"]
		p.AccessKeySecret = v["secret"]
		p.SecurityToken = v["token"]
		a.async(func(ctx context.Context) (func(), error) {
			saved, e := a.accounts.Save(p)
			if e != nil {
				return nil, e
			}
			profiles, e := a.accounts.List()
			return func() {
				a.accountSaved(saved, profiles)
				a.dialog = nil
				a.notice = "账号已加密保存"
			}, e
		})
		return
	case "certificate":
		r := certificates.Request{Domains: strings.FieldsFunc(v["domains"], func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ' ' }), Email: strings.TrimSpace(v["email"]), AcceptTerms: f.Terms, Staging: f.Staging}
		if !r.AcceptTerms {
			a.err = "请先阅读并同意 Let’s Encrypt 服务条款"
			return
		}
		b, _ := json.Marshal(r)
		_, e := a.jobs.Submit(p.ID, "certificate", "签发 "+v["domains"], b)
		if e != nil {
			a.err = a.safeError(e)
			return
		}
		a.dialog = nil
		a.showTasks = true
		return
	case "object-link":
		a.copyObjectLink(f)
		return
	case "whitelist":
		a.saveWhitelistTarget(f, v)
		return
	}
	if f.Kind == "move" {
		a.dialog = nil
		a.confirm = &confirmation{Title: "确认移动对象", Detail: fmt.Sprintf("账号：%s\nBucket：%s\n%s → %s\n复制完成后将删除原对象。", p.Name, bucket, v["source"], v["destination"]), Run: func() { a.mutateObject(p, bucket, f.Kind, v) }}
		return
	}
	a.async(func(ctx context.Context) (func(), error) {
		var e error
		switch f.Kind {
		case "bucket":
			p.Region = v["region"]
			c, err := a.p.Storage(p)
			e = err
			if e == nil {
				e = c.CreateBucket(ctx, v["name"], v["region"])
			}
		case "directory":
			c, err := a.p.Storage(p)
			e = err
			if e == nil {
				e = c.CreateDirectory(ctx, bucket, v["key"])
			}
		case "copy", "metadata":
			return a.objectMutation(ctx, p, bucket, f.Kind, v)
		case "domain":
			d, err := a.p.DNS(p)
			e = err
			if e == nil {
				e = d.AddDomain(ctx, strings.TrimSpace(v["domain"]))
			}
		case "record":
			ttl, err := strconv.Atoi(v["ttl"])
			if err != nil {
				return nil, errors.New("TTL 必须是整数")
			}
			priority, _ := strconv.Atoi(v["priority"])
			d, err := a.p.DNS(p)
			e = err
			if e == nil {
				_, e = d.AddRecord(ctx, cloud.Record{Domain: domain, RR: v["rr"], Type: v["type"], Value: v["value"], TTL: ttl, Priority: priority, Line: "default"})
			}
		}
		return func() { a.dialog = nil; a.notice = "操作成功"; a.refresh() }, e
	})
}
func (a *App) mutateObject(p cloud.Account, bucket, kind string, v map[string]string) {
	a.async(func(ctx context.Context) (func(), error) { return a.objectMutation(ctx, p, bucket, kind, v) })
}
func (a *App) objectMutation(ctx context.Context, p cloud.Account, bucket, kind string, v map[string]string) (func(), error) {
	c, e := a.p.Storage(p)
	if e != nil {
		return nil, e
	}
	switch kind {
	case "metadata":
		var meta map[string]string
		if e = json.Unmarshal([]byte(v["metadata"]), &meta); e != nil {
			return nil, errors.New("元数据必须是 JSON 字符串字典")
		}
		e = c.SetMetadata(ctx, bucket, v["source"], meta)
	case "copy", "move":
		if v["source"] == v["destination"] || v["destination"] == "" {
			return nil, errors.New("请输入不同的目标对象路径")
		}
		before, err := c.Stat(ctx, bucket, v["source"])
		if err != nil {
			return nil, err
		}
		e = c.CopyObject(ctx, bucket, v["source"], v["destination"])
		if e == nil && kind == "move" {
			after, err := c.Stat(ctx, bucket, v["source"])
			if err != nil {
				return nil, err
			}
			dest, err := c.Stat(ctx, bucket, v["destination"])
			if err != nil {
				return nil, err
			}
			if after.ETag != before.ETag || after.VersionID != before.VersionID || after.Size != before.Size || !sameContent(before, dest) {
				return nil, errors.New("复制期间对象发生变化，已保留原对象")
			}
			manifest, err := storage.NewDeleteManifest(p.ID, bucket, []string{v["source"]})
			if err != nil {
				return nil, err
			}
			e = storage.ConfirmDelete(ctx, c, manifest, p.ID, bucket, manifest.Keys())
		}
	}
	return func() { a.dialog = nil; a.notice = "操作成功"; a.refreshOSS(false) }, e
}

func sameContent(source, destination cloud.Object) bool {
	if source.Size != destination.Size {
		return false
	}
	if source.CRC64 != "" && destination.CRC64 != "" {
		return source.CRC64 == destination.CRC64
	}
	return source.ETag != "" && source.ETag == destination.ETag
}
func (a *App) accountSaved(saved cloud.Account, profiles []cloud.Account) {
	a.profiles = profiles
	a.generation++
	a.accountID = saved.ID
	a.region = saved.Region
	a.resetResources()
}
func (a *App) inspectObject(o cloud.Object) {
	p, e := a.storageAccount()
	if e != nil {
		return
	}
	bucket := a.bucket
	a.async(func(ctx context.Context) (func(), error) {
		c, e := a.p.Storage(p)
		if e != nil {
			return nil, e
		}
		obj, e := c.Stat(ctx, bucket, o.Key)
		return func() {
			meta, _ := json.MarshalIndent(obj.Metadata, "", "  ")
			a.newForm("metadata", "对象属性与元数据", map[string]string{"source": obj.Key, "size": formatBytes(obj.Size), "etag": obj.ETag, "metadata": string(meta)})
		}, e
	})
}
func (a *App) lookupDNS() {
	domain := a.domain
	if domain == "" {
		return
	}
	a.async(func(ctx context.Context) (func(), error) {
		rs, e := net.DefaultResolver.LookupHost(ctx, domain)
		return func() { a.queryResult = domain + "  →  " + strings.Join(rs, ", ") }, e
	})
}
func (a *App) toggleAutomatic(index int) {
	if index < 0 || index >= len(a.rules) {
		return
	}
	next := append([]cloud.Target(nil), a.rules...)
	next[index].Automatic = !next[index].Automatic
	if next[index].Automatic {
		for i, t := range next {
			if i != index && t.Automatic && ruleScopeKey(t) == ruleScopeKey(next[index]) {
				a.err = "这个资源和端口已有自动跟随目标；其他 IP 请使用手动指定模式"
				return
			}
		}
	}
	if !a.persistWhitelistTargets(next) {
		return
	}
	a.autoNext = time.Now()
}
func (a *App) detectIP() {
	a.async(func(ctx context.Context) (func(), error) {
		ip, e := aliyun.PublicIP(ctx)
		return func() { a.publicIP = ip }, e
	})
}
func (a *App) addWhitelist(targets []cloud.Target, automatic bool) {
	if len(targets) == 0 {
		return
	}
	if automatic {
		a.runAutomaticWhitelist(targets, aliyun.PublicIP)
		return
	}
	a.runManualWhitelist(targets, aliyun.PublicIP)
}

func (a *App) autoTick() {
	if a.locked || a.accounts == nil || time.Now().Before(a.autoNext) {
		return
	}
	a.autoNext = time.Now().Add(time.Minute)
	var targets []cloud.Target
	for _, t := range a.rules {
		if t.Automatic {
			targets = append(targets, t)
		}
	}
	if len(targets) > 0 && a.busy == 0 {
		a.addWhitelist(targets, true)
	}
}
func (a *App) viewRules(t cloud.Target) {
	if t.Automatic && t.Kind != "ecs" {
		t.Group = ""
	}
	a.openRuleDetails(t)
}
func (a *App) renewCertificate(c certificates.Certificate) {
	b, _ := json.Marshal(c.ID)
	if _, e := a.jobs.Submit(c.AccountID, "renew", "续期 "+strings.Join(c.Domains, ", "), b); e != nil {
		a.err = a.safeError(e)
	} else {
		a.showTasks = true
	}
}
func (a *App) exportCertificate(c certificates.Certificate) {
	a.async(func(ctx context.Context) (func(), error) {
		if a.p.PickFolder == nil {
			return nil, errors.New("目录选择不可用")
		}
		dir, e := a.p.PickFolder()
		if e != nil || dir == "" {
			return nil, e
		}
		e = a.certs.Export(c.AccountID, c.ID, dir)
		return func() { a.notice = "证书与私钥已导出到 " + dir }, e
	})
}
func (a *App) cleanupCertificates() {
	p, e := a.current()
	if e != nil {
		return
	}
	a.async(func(ctx context.Context) (func(), error) {
		e := a.certs.Cleanup(ctx, p)
		return func() { a.notice = "已检查并清理本工具拥有的验证 TXT" }, e
	})
}
func parentPrefix(s string) string {
	s = strings.TrimSuffix(s, "/")
	p := path.Dir(s)
	if p == "." || p == "/" {
		return ""
	}
	return p + "/"
}
