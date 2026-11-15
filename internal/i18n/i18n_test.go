package i18n_test

import (
	"reflect"
	"strings"
	"testing"

	"hellauncher/internal/catalog"
	"hellauncher/internal/hud"
	"hellauncher/internal/i18n"
)

func TestAllIconsHaveLocalizedOfficialNamesCodesAndEmbeddedSVGs(t *testing.T) {
	if len(i18n.Icons) != 111 {
		t.Fatalf("expected all 111 official Stratagem presets, got %d", len(i18n.Icons))
	}
	seen := make(map[string]bool, len(i18n.Icons))
	for _, ic := range i18n.Icons {
		if strings.HasPrefix(ic.Key, "tactical-") {
			t.Fatalf("non-canon custom icon %q must not remain in i18n.Icons", ic.Key)
		}
		if seen[ic.Key] {
			t.Fatalf("duplicate icon key %q", ic.Key)
		}
		seen[ic.Key] = true

		zh := i18n.IconName(ic.Key, catalog.LangZH)
		en := i18n.IconName(ic.Key, catalog.LangEN)
		if zh == "" || en == "" {
			t.Fatalf("empty localized name for %q: zh=%q en=%q", ic.Key, zh, en)
		}
		if strings.Contains(zh, ".svg") || strings.Contains(en, ".svg") {
			t.Fatalf("raw filename leaked into localized icon name for %q: zh=%q en=%q", ic.Key, zh, en)
		}
		if len(ic.Code) == 0 {
			t.Fatalf("missing canonical arrow sequence for %q", ic.Key)
		}
		if !hud.HasIconSVG(ic.Key) {
			t.Fatalf("hud.HasIconSVG(%q) returned false (missing embedded SVG)", ic.Key)
		}
		if hud.IconSVG(ic.Key) == nil {
			t.Fatalf("hud.IconSVG(%q) returned nil", ic.Key)
		}
	}
}

func TestFormatErrorTranslatesConflictAndValidationErrors(t *testing.T) {
	ce := &catalog.ConflictError{
		FirstName:  "Resupply",
		SecondName: "Join",
		Prefix:     []catalog.Dir{catalog.DirDown, catalog.DirDown},
	}
	zhMsg := i18n.FormatError(ce, catalog.LangZH)
	enMsg := i18n.FormatError(ce, catalog.LangEN)
	if !strings.Contains(zhMsg, "冲突") || !strings.Contains(zhMsg, "↓↓") {
		t.Fatalf("unexpected ZH conflict message: %q", zhMsg)
	}
	if !strings.Contains(enMsg, "Sequence conflict") || !strings.Contains(enMsg, "↓↓") {
		t.Fatalf("unexpected EN conflict message: %q", enMsg)
	}

	ve := &catalog.ValidationError{
		StratagemID: "x",
		Code:        catalog.ValEmptyStepTarget,
		Reason:      "动作目标不能为空",
	}
	if got := i18n.FormatError(ve, catalog.LangEN); got != "Action target is required" {
		t.Fatalf("unexpected EN validation error: %q", got)
	}

	sysVe := &catalog.ValidationError{
		StratagemID: catalog.SystemJoinHelldiversID,
		Code:        catalog.ValSystemProtected,
		Reason:      "真理部指令",
	}
	if got := i18n.FormatError(sysVe, catalog.LangEN); !strings.Contains(got, "Ministry of Truth") {
		t.Fatalf("unexpected EN system protected error: %q", got)
	}
}

func TestAllStringFieldsPopulatedInBothLanguages(t *testing.T) {
	for _, lang := range []catalog.Language{catalog.LangZH, catalog.LangEN} {
		tbl := i18n.For(lang)
		rv := reflect.ValueOf(tbl)
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			f := rt.Field(i)
			if f.Type.Kind() != reflect.String {
				continue
			}
			val := strings.TrimSpace(rv.Field(i).String())
			if val == "" {
				t.Fatalf("empty string field %s in language %s", f.Name, lang)
			}
		}
	}

	if len(i18n.SystemTactics) < 12 {
		t.Fatalf("expected >=12 built-in SystemTactics, got %d", len(i18n.SystemTactics))
	}
	for _, tac := range i18n.SystemTactics {
		if !tac.IsCloseWindow && !tac.Action.Valid() {
			t.Fatalf("invalid SystemAction in SystemTactics: %+v", tac)
		}
		if tac.Name(catalog.LangZH) == "" || tac.Name(catalog.LangEN) == "" {
			t.Fatalf("missing localized name for SystemTactic: %+v", tac)
		}
	}
}
