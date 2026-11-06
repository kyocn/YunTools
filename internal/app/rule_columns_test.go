package app

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

func ruleColumnFixture(t *testing.T, width int) (*App, *ui.Tester) {
	t.Helper()
	a := New(Platform{Store: &memStore{}})
	t.Cleanup(a.Close)
	a.accountID = "a"
	a.ruleDetail = &ruleDetails{Target: cloud.Target{AccountID: "a", Kind: "ecs", Name: "测试安全组", ID: "sg-test"}, Direction: "全部方向", Rules: []cloud.SecurityGroupRule{{ID: "sgr-example-123456789012345", Direction: "ingress", Policy: "Accept", Protocol: "TCP", Ports: "65535/65535", Source: "192.0.2.123/32", Priority: "1", Created: time.Date(2026, 11, 6, 6, 8, 0, 0, time.UTC), Description: "允许管理员访问"}}}
	tt := ui.NewTester(a.View, width, 820)
	tt.Frame()
	return a, tt
}

func columnResizeEdge(t *testing.T, tt *ui.Tester, title, next string) (float32, float32) {
	t.Helper()
	h, ok := tt.Find(title)
	if !ok {
		t.Fatal("missing header", title)
	}
	n, ok := tt.Find(next)
	if !ok {
		t.Fatal("missing next header", next)
	}
	y := h.Y + h.H/2
	for x := h.X + h.W; x < n.X; x++ {
		tt.Move(x, y)
		if tt.Cursor() == ui.CursorResizeEW {
			return x, y
		}
	}
	t.Fatal("header has no draggable resize edge", title)
	return 0, 0
}

func TestSecurityRuleColumnDragResetAndReopen(t *testing.T) {
	a, tt := ruleColumnFixture(t, 1280)
	before, _ := tt.Find("65535/65535")
	x, y := columnResizeEdge(t, tt, "端口", "来源 / 目标")
	tt.Press(x, y)
	tt.Move(x+70, y)
	tt.Release(x+70, y)
	tt.Frame()
	after, _ := tt.Find("65535/65535")
	if math.Abs(float64(after.W-before.W-70)) > 2 {
		t.Fatalf("drag did not resize cell: %v -> %v", before, after)
	}
	// A double click on the boundary fits the widened column to its contents.
	x, y = columnResizeEdge(t, tt, "端口", "来源 / 目标")
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	fitted, _ := tt.Find("65535/65535")
	if fitted.W >= after.W || fitted.W < 80 {
		t.Fatalf("double click did not fit port content: %v", fitted)
	}
	// Widen again to verify remembered user layout on reopening.
	x, y = columnResizeEdge(t, tt, "端口", "来源 / 目标")
	tt.Press(x, y)
	tt.Move(x+70, y)
	tt.Release(x+70, y)
	tt.Frame()
	after, _ = tt.Find("65535/65535")
	d := a.ruleDetail
	a.ruleDetail = nil
	tt.Frame()
	a.ruleDetail = d
	tt.Frame()
	reopened, _ := tt.Find("65535/65535")
	if math.Abs(float64(reopened.W-after.W)) > 1 {
		t.Fatal("reopening lost column width")
	}
	if err := tt.Click("重置列宽"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	reset, _ := tt.Find("65535/65535")
	if math.Abs(float64(reset.W-before.W)) > 1 {
		t.Fatal("reset did not restore default column width")
	}
}

func TestSecurityRuleColumnDefaultsAndHorizontalScroll(t *testing.T) {
	for _, width := range []int{960, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			_, tt := ruleColumnFixture(t, width)
			ports, _ := tt.Find("65535/65535")
			if ports.W < 80 {
				t.Fatalf("port range is squeezed: %v", ports)
			}
			if width == 960 {
				peer, _ := tt.Find("192.0.2.123/32")
				tt.Scroll(peer.X+10, peer.Y+10, 600, 0)
				tt.Frame()
				id, ok := tt.Find("sgr-example-123456789012345")
				if !ok || id.X < 0 || id.X+id.W > 960 {
					t.Fatalf("rightmost column not reachable: %v", id)
				}
				h, _ := tt.Find("描述 / 规则 ID")
				if math.Abs(float64(h.X-id.X)) > 1 {
					t.Fatal("header did not follow horizontal scroll")
				}
			}
			if dir := os.Getenv("YUNTOOLS_RENDER_DIR"); dir != "" {
				os.MkdirAll(dir, 0700)
				f, e := os.Create(filepath.Join(dir, fmt.Sprintf("rule-columns-%d.png", width)))
				if e != nil {
					t.Fatal(e)
				}
				e = png.Encode(f, tt.Image())
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
