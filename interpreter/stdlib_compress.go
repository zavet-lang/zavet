package interpreter

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	registerCompressModule()
}

// registerCompressModule регистрирует стандартный модуль «сжатие» (gzip, zlib, zip).
func registerCompressModule() {
	fns := map[string]func(args ...Object) Object{
		// сжатие.gzip_сжать(строка) -> base64-строка
		"gzip_сжать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("сжатие.gzip_сжать", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.gzip_сжать", 1, "строка", args[0].Type())
			}
			var buf bytes.Buffer
			w := gzip.NewWriter(&buf)
			if _, err := w.Write([]byte(s.Value)); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех сжатия gzip: "+err.Error(), "")
			}
			if err := w.Close(); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех закрытия gzip: "+err.Error(), "")
			}
			return &String{Value: base64.StdEncoding.EncodeToString(buf.Bytes())}
		},

		// сжатие.gzip_распаковать(base64) -> строка
		"gzip_распаковать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("сжатие.gzip_распаковать", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.gzip_распаковать", 1, "строка", args[0].Type())
			}
			raw, err := base64.StdEncoding.DecodeString(s.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "неверный base64: "+err.Error(), "")
			}
			r, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех распаковки gzip: "+err.Error(), "")
			}
			defer r.Close()
			out, err := io.ReadAll(r)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог прочесть gzip: "+err.Error(), "")
			}
			return &String{Value: string(out)}
		},

		// сжатие.zlib_сжать(строка) -> base64-строка
		"zlib_сжать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("сжатие.zlib_сжать", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.zlib_сжать", 1, "строка", args[0].Type())
			}
			var buf bytes.Buffer
			w := zlib.NewWriter(&buf)
			if _, err := w.Write([]byte(s.Value)); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех сжатия zlib: "+err.Error(), "")
			}
			if err := w.Close(); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех закрытия zlib: "+err.Error(), "")
			}
			return &String{Value: base64.StdEncoding.EncodeToString(buf.Bytes())}
		},

		// сжатие.zlib_распаковать(base64) -> строка
		"zlib_распаковать": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("сжатие.zlib_распаковать", 1, len(args))
			}
			s, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.zlib_распаковать", 1, "строка", args[0].Type())
			}
			raw, err := base64.StdEncoding.DecodeString(s.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "неверный base64: "+err.Error(), "")
			}
			r, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех распаковки zlib: "+err.Error(), "")
			}
			defer r.Close()
			out, err := io.ReadAll(r)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог прочесть zlib: "+err.Error(), "")
			}
			return &String{Value: string(out)}
		},

		// сжатие.zip_создать(путь_архива, словарь_имя_содержимое | массив_путей) -> истина
		"zip_создать": func(args ...Object) Object {
			if err := denyIfSafe("сжатие.zip_создать"); err != nil {
				return err
			}
			if len(args) != 2 {
				return builtinErrorWrongArgCount("сжатие.zip_создать", 2, len(args))
			}
			dest, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.zip_создать", 1, "строка", args[0].Type())
			}
			f, err := os.Create(dest.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог создать архив: "+err.Error(), "")
			}
			defer f.Close()
			zw := zip.NewWriter(f)

			switch src := args[1].(type) {
			case *Hash:
				for _, p := range src.orderedPairs() {
					w, err := zw.Create(p.Key.Inspect())
					if err != nil {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "грех записи в zip: "+err.Error(), "")
					}
					if _, err := w.Write([]byte(p.Value.Inspect())); err != nil {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "грех записи в zip: "+err.Error(), "")
					}
				}
			case *Array:
				for _, el := range src.Elements {
					ps, ok := el.(*String)
					if !ok {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "в массиве путей все элементы должны быть строками", "")
					}
					data, err := os.ReadFile(ps.Value)
					if err != nil {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "не смог прочесть файл: "+err.Error(), "")
					}
					w, err := zw.Create(filepath.Base(ps.Value))
					if err != nil {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "грех записи в zip: "+err.Error(), "")
					}
					if _, err := w.Write(data); err != nil {
						zw.Close()
						return ErrorWithHint(getCurrentCallToken(), "грех записи в zip: "+err.Error(), "")
					}
				}
			default:
				zw.Close()
				return ErrorWithHint(getCurrentCallToken(), "второе приношение должно быть словарём {имя: содержимое} или массивом путей", "")
			}

			if err := zw.Close(); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "грех закрытия zip: "+err.Error(), "")
			}
			return TRUE
		},

		// сжатие.zip_список(путь_архива) -> массив имён файлов
		"zip_список": func(args ...Object) Object {
			if len(args) != 1 {
				return builtinErrorWrongArgCount("сжатие.zip_список", 1, len(args))
			}
			p, ok := args[0].(*String)
			if !ok {
				return builtinErrorWrongArgType("сжатие.zip_список", 1, "строка", args[0].Type())
			}
			if err := CheckPath(p.Value); err != nil {
				return err
			}
			zr, err := zip.OpenReader(p.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог открыть zip: "+err.Error(), "")
			}
			defer zr.Close()
			elems := make([]Object, 0, len(zr.File))
			for _, f := range zr.File {
				elems = append(elems, &String{Value: f.Name})
			}
			return &Array{Elements: elems}
		},

		// сжатие.zip_распаковать(путь_архива, папка) -> массив распакованных путей
		"zip_распаковать": func(args ...Object) Object {
			if err := denyIfSafe("сжатие.zip_распаковать"); err != nil {
				return err
			}
			if len(args) != 2 {
				return builtinErrorWrongArgCount("сжатие.zip_распаковать", 2, len(args))
			}
			srcPath, ok1 := args[0].(*String)
			destDir, ok2 := args[1].(*String)
			if !ok1 || !ok2 {
				return ErrorWithHint(getCurrentCallToken(), "оба приношения должны быть строками", "")
			}
			zr, err := zip.OpenReader(srcPath.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог открыть zip: "+err.Error(), "")
			}
			defer zr.Close()

			absDest, err := filepath.Abs(destDir.Value)
			if err != nil {
				return ErrorWithHint(getCurrentCallToken(), "неверный путь назначения: "+err.Error(), "")
			}
			if err := os.MkdirAll(absDest, 0755); err != nil {
				return ErrorWithHint(getCurrentCallToken(), "не смог создать папку: "+err.Error(), "")
			}

			extracted := []Object{}
			for _, f := range zr.File {
				target := filepath.Join(absDest, f.Name)
				// Защита от Zip Slip
				if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(absDest)+string(os.PathSeparator)) &&
					filepath.Clean(target) != filepath.Clean(absDest) {
					return ErrorWithHint(getCurrentCallToken(), "опасный путь внутри архива: "+f.Name, "")
				}
				if f.FileInfo().IsDir() {
					_ = os.MkdirAll(target, 0755)
					continue
				}
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return ErrorWithHint(getCurrentCallToken(), "не смог создать папку: "+err.Error(), "")
				}
				rc, err := f.Open()
				if err != nil {
					return ErrorWithHint(getCurrentCallToken(), "грех чтения из zip: "+err.Error(), "")
				}
				out, err := os.Create(target)
				if err != nil {
					rc.Close()
					return ErrorWithHint(getCurrentCallToken(), "не смог создать файл: "+err.Error(), "")
				}
				_, copyErr := io.Copy(out, rc)
				out.Close()
				rc.Close()
				if copyErr != nil {
					return ErrorWithHint(getCurrentCallToken(), "грех распаковки файла: "+copyErr.Error(), "")
				}
				extracted = append(extracted, &String{Value: target})
			}
			return &Array{Elements: extracted}
		},
	}

	stdModules["сжатие"] = makeHashFromBuiltins(fns)
}
