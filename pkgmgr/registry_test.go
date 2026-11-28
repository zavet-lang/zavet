package pkgmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRegistryMatchesJSON(t *testing.T) {
	def := DefaultRegistry()
	if len(def.Packages) == 0 {
		t.Fatal("DefaultRegistry() is empty")
	}

	raw, err := os.ReadFile(filepath.Join("..", "registry", "registry.json"))
	if err != nil {
		t.Fatalf("read registry.json: %v", err)
	}
	var fileReg Registry
	if err := json.Unmarshal(raw, &fileReg); err != nil {
		t.Fatalf("parse registry.json: %v", err)
	}

	if len(def.Packages) != len(fileReg.Packages) {
		t.Fatalf("DefaultRegistry has %d packages, registry.json has %d", len(def.Packages), len(fileReg.Packages))
	}
	for name, entry := range def.Packages {
		fEntry, ok := fileReg.Packages[name]
		if !ok {
			t.Fatalf("package %q missing in registry.json", name)
		}
		if entry.URL != fEntry.URL || entry.Description != fEntry.Description || entry.Category != fEntry.Category {
			t.Fatalf("package %q differs between Go and JSON: %+v vs %+v", name, entry, fEntry)
		}
	}
}

func TestRegistrySearchAndResolve(t *testing.T) {
	reg := DefaultRegistry()
	if got := reg.ResolveName("тг"); got != "https://github.com/zavet-lang/zavet-tg" {
		t.Fatalf("unexpected URL for тг: %q", got)
	}
	if got := reg.ResolveName("несуществующий"); got != "" {
		t.Fatalf("expected empty URL for unknown package, got %q", got)
	}

	found := reg.Search("Telegram")
	wantTelegram := map[string]bool{"тг": true, "telegram-меню": true}
	gotTelegram := map[string]bool{}
	for _, name := range found {
		gotTelegram[name] = true
	}
	for name := range wantTelegram {
		if !gotTelegram[name] {
			t.Fatalf("Search(\"Telegram\") missing %q in %v", name, found)
		}
	}
	if len(found) < len(wantTelegram) {
		t.Fatalf("unexpected Search(\"Telegram\") result: %v", found)
	}

	all := reg.Search("")
	if len(all) != len(reg.Packages) {
		t.Fatalf("Search(\"\") returned %d, want %d", len(all), len(reg.Packages))
	}
}

func TestIsShortName(t *testing.T) {
	if !IsShortName("тг") || !IsShortName("тг@v1.0.0") {
		t.Fatal("expected short names to be recognized")
	}
	if IsShortName("github.com/zavet-lang/zavet-tg") || IsShortName("https://github.com/a/b") {
		t.Fatal("expected URLs not to be short names")
	}
}
