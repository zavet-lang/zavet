package interpreter

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
)

//go:embed fonts/fa-solid-900.ttf
var faSolidTTF []byte

var (
	faFontOnce sync.Once
	faFontPath string
)

func ensureFAFontFile() (string, error) {
	var err error
	faFontOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), "zavet-fonts")
		_ = os.MkdirAll(dir, 0755)
		path := filepath.Join(dir, "fa-solid-900.ttf")
		if st, e := os.Stat(path); e != nil || st.Size() == 0 {
			if e := os.WriteFile(path, faSolidTTF, 0644); e != nil {
				err = e
				return
			}
		}
		faFontPath = path
	})
	return faFontPath, err
}
