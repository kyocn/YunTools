package icons

import (
	"embed"
	"github.com/egoist/mygo/ui"
	"sync"
)

//go:embed svg/*.svg
var files embed.FS
var cache sync.Map

func Get(name string) *ui.SVG {
	if v, ok := cache.Load(name); ok {
		return v.(*ui.SVG)
	}
	b, e := files.ReadFile("svg/" + name + ".svg")
	if e != nil {
		return nil
	}
	s := ui.MustParseSVG(b)
	cache.Store(name, s)
	return s
}
