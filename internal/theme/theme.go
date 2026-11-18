package theme

import "github.com/egoist/mygo/ui"

const (
	Body   float32 = 13
	Small  float32 = 11
	Title  float32 = 22
	Radius float32 = 12
)

var Accent = ui.Hex("#1984FC")

func Space(n float32) float32 { return n * 4 }

type Palette struct{ BG, Sidebar, Card, Field, Line, Text, Muted, Accent, Green, Orange, Red ui.Color }

func Colors(dark bool) Palette {
	// Canvas, sidebar and inset follow the same quiet split as SSHYard:
	// the sidebar is a step darker than the page, cards sit on the canvas.
	if dark {
		return Palette{ui.Hex("#1E1E20"), ui.Hex("#28282A"), ui.Hex("#2A2A2D"), ui.Hex("#333336"), ui.Hex("#3A3A3D"), ui.Hex("#F5F5F7"), ui.Hex("#98989D"), Accent, ui.Hex("#30D158"), ui.Hex("#FF9F0A"), ui.Hex("#FF453A")}
	}
	return Palette{ui.Hex("#F5F5F7"), ui.Hex("#EDEDF0"), ui.Hex("#FFFFFF"), ui.Hex("#F2F2F4"), ui.Hex("#DCDCE0"), ui.Hex("#1D1D1F"), ui.Hex("#67676C"), Accent, ui.Hex("#24A148"), ui.Hex("#D97E00"), ui.Hex("#E5342A")}
}
func Apply(base ui.Theme, dark bool) *ui.Theme {
	p := Colors(dark)
	base.Dark = dark
	base.Background = p.BG
	base.Surface = p.Field
	base.SurfaceHover = p.Line
	base.SurfacePressed = p.Accent.Alpha(.18)
	base.Text = p.Text
	base.TextMuted = p.Muted
	base.Border = p.Line
	base.Accent = p.Accent
	base.AccentHover = ui.Hex("#2A8DFC")
	base.AccentPressed = ui.Hex("#1570D6")
	base.AccentText = ui.Hex("#FFFFFF")
	base.Selection = p.Accent.Alpha(.22)
	base.Focus = p.Accent
	base.Danger = p.Red
	base.Success = p.Green
	base.Warning = p.Orange
	base.FontSize = 13
	base.Radius = 8
	base.Spacing = 4
	return &base
}
