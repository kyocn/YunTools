// Generates the repository's vector-derived application icon.
package main

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"yuntools/internal/icons"
)

func main() {
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Padding(50).Children(func() {
			ui.Box(c).Fill().Radius(92).Background(ui.Hex("#1984FC")).Center().Children(func() { ui.Icon(c, icons.Get("cloud-line")).Size(280, 280).TextColor(ui.Hex("#FFFFFF")) })
		})
	}, 512, 512, 2)
	f, e := os.Create("resources/icon.png")
	if e != nil {
		panic(e)
	}
	defer f.Close()
	if e = png.Encode(f, img); e != nil {
		panic(e)
	}
}
