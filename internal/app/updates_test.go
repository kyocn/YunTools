package app

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestUpdateSettingsUseRuntimeVersionAndNativeCallbacks(t *testing.T) {
	checks := 0
	automatic := true
	a := New(Platform{Store: &memStore{}, Version: "0.2.7", CheckUpdates: func() { checks++ },
		UpdateStatus: func() UpdateStatus {
			return UpdateStatus{Enabled: true, Automatic: automatic, Repository: "kyocn/YunTools"}
		},
		SetAutomaticUpdates: func(on bool) { automatic = on },
	})
	defer a.Close()
	a.page = "settings"
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if !tt.HasText("v0.2.7") || tt.HasText("v0.1.0") {
		t.Fatal("version not obtained from build metadata")
	}
	if !tt.HasText("kyocn/YunTools") {
		t.Fatal("update repository not shown")
	}
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatal("native updater callback not invoked")
	}
	if err := tt.Click("自动检查更新"); err != nil {
		t.Fatal(err)
	}
	if automatic {
		t.Fatal("automatic update preference not propagated")
	}
}
