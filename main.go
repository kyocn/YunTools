package main

import (
	"errors"
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"yuntools/internal/app"
	"yuntools/internal/cloud"
	"yuntools/internal/keychain"
)

type systemSecrets struct{}

// Keep the existing OS identity so upgrading to YunTools can unlock old vaults.
const secretService = "dev.aliyuntools.desktop"

func (systemSecrets) Get(k string) ([]byte, error) {
	b, e := mygo.Secrets.Service(secretService).Get(k)
	if errors.Is(e, mygo.ErrSecretNotFound) {
		return nil, keychain.ErrNotFound
	}
	return b, e
}
func (systemSecrets) Set(k string, v []byte) error {
	return mygo.Secrets.Service(secretService).Set(k, v)
}
func (systemSecrets) Delete(k string) error {
	return mygo.Secrets.Service(secretService).Delete(k)
}

func main() {
	m := metadata()
	mygo.App.SetName(m.Name)
	mygo.App.SetVersion(m.Version)
	mygo.Use(native.New(updater.Options{Language: "zh-CN"}))
	mygo.App.WhenReady(func() {
		root, e := os.UserConfigDir()
		if e != nil {
			log.Fatal("无法定位应用数据目录")
		}
		dir, e := dataDirectory(root, os.Getenv("YUNTOOLS_DATA_DIR"), os.Getenv("ALIYUNTOOLS_DATA_DIR"))
		if e != nil {
			log.Fatal("无法定位账号保险库")
		}
		var mu sync.Mutex
		var vault *keychain.Store
		p := app.Platform{DataDir: dir, Post: func(fn func()) { go mygo.RunOnMain(fn) }, Unlock: func() (cloud.SecretStore, error) {
			mu.Lock()
			defer mu.Unlock()
			if vault != nil {
				return vault, nil
			}
			v, e := keychain.Open(dir, systemSecrets{})
			if e == nil {
				vault = v
			}
			return v, e
		}, PickFiles: func() ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择上传文件", Multiple: true})
		}, PickFolder: func() (string, error) {
			paths, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择文件夹", Directory: true, CreateDirectories: true})
			if e != nil || len(paths) == 0 {
				return "", e
			}
			return paths[0], nil
		}, SaveFile: func(name string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "保存文件", DefaultPath: name})
		}}
		p.CopyText = func(text string) error { return mygo.Clipboard.Write(transfer.TextData(text)) }
		p.Version = m.Version
		p.CheckUpdates = updater.CheckForUpdates
		p.SetAutomaticUpdates = updater.SetAutomaticChecks
		p.UpdateStatus = func() app.UpdateStatus {
			return app.UpdateStatus{Enabled: mygo.Updater.Enabled(), Automatic: updater.AutomaticChecks(),
				Repository: m.Updates.GitHub, AvailableVersion: updater.AvailableVersion(), LastCheck: updater.LastCheck()}
		}
		a := app.New(p)
		vibrancy := mygo.VibrancyNone
		if runtime.GOOS == "darwin" {
			vibrancy = mygo.VibrancySidebar
		}
		// Let AppKit position its standard buttons and maintain their hover tracking.
		// The content title bar reads the native height/insets instead of moving them.
		mygo.NewWindow(mygo.WindowOptions{Title: "YunTools", Width: 1280, Height: 820, MinWidth: 960, MinHeight: 640, TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 48, Vibrancy: vibrancy, BackgroundColor: "#22252C", Content: ui.View(a.View)})
		mygo.App.OnQuit(func() {
			a.Close()
			mu.Lock()
			defer mu.Unlock()
			if vault != nil {
				vault.Close()
			}
		})
	})
	if e := mygo.App.Run(); e != nil {
		log.Fatal(e)
	}
}
