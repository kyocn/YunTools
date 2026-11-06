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
	if dark {
		return Palette{ui.Hex("#191B20"), ui.Hex("#22252C"), ui.Hex("#24272F"), ui.Hex("#2D313B"), ui.Hex("#363B47"), ui.Hex("#F1F3F8"), ui.Hex("#939BAD"), Accent, ui.Hex("#43C99A"), ui.Hex("#F0AE54"), ui.Hex("#F5727C")}
	}
	return Palette{ui.Hex("#F5F6FA"), ui.Hex("#EDEFF5"), ui.Hex("#FFFFFF"), ui.Hex("#F0F2F7"), ui.Hex("#DFE3EC"), ui.Hex("#202533"), ui.Hex("#738097"), Accent, ui.Hex("#16875D"), ui.Hex("#B97916"), ui.Hex("#CB384A")}
}
func Apply(base ui.Theme, dark bool) *ui.Theme {
	p := Colors(dark)
	base.Dark = dark
	base.Background = p.BG
	base.Surface = p.Field
	base.SurfaceHover = p.Line
	base.SurfacePressed = p.Accent.Alpha(.2)
	base.Text = p.Text
	base.TextMuted = p.Muted
	base.Border = p.Line
	base.Accent = p.Accent
	base.AccentHover = ui.Hex("#3997FF")
	base.AccentPressed = ui.Hex("#116ED8")
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
