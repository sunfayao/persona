package assistant

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
)

// The launchers' keys are sealed in settings (launchersManagedKey): the key rotation re-seals the fields listed for it
// in db.SealedSettings, so every sealed field of storedLauncher must be there.
func TestTheLaunchersSealedFieldsAreReSealed(t *testing.T) {
	i := slices.IndexFunc(db.SealedSettings(), func(s db.SealedSetting) bool { return s.Key == launchersManagedKey })
	if i < 0 || !db.SealedSettings()[i].List {
		t.Fatalf("%s is not a list in db.SealedSettings", launchersManagedKey)
	}
	fields := db.SealedSettings()[i].Fields
	sealed := sealedJSONFields(reflect.TypeOf(storedLauncher{}))
	if len(sealed) == 0 {
		t.Fatal("no sealed field found in storedLauncher")
	}
	for _, name := range sealed {
		if !slices.Contains(fields, name) {
			t.Errorf("storedLauncher's %s is sealed and not re-sealed by the key rotation", name)
		}
	}
}

// A provider's keys for single computers are sealed in their own setting: the rotation re-seals them too.
func TestTheComputersKeysAreReSealed(t *testing.T) {
	i := slices.IndexFunc(db.SealedSettings(), func(s db.SealedSetting) bool { return s.Key == launchersMachineKeysKey })
	if i < 0 || !db.SealedSettings()[i].List {
		t.Fatalf("%s is not a list in db.SealedSettings", launchersMachineKeysKey)
	}
	for _, name := range sealedJSONFields(reflect.TypeOf(machineKey{})) {
		if !slices.Contains(db.SealedSettings()[i].Fields, name) {
			t.Errorf("machineKey's %s is sealed and not re-sealed by the key rotation", name)
		}
	}
}

// sealedJSONFields are the JSON names of typ's fields that say they hold a sealed value, embedded structs' too (their
// fields are the object's in JSON).
func sealedJSONFields(typ reflect.Type) []string {
	var out []string
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			out = append(out, sealedJSONFields(f.Type)...)
			continue
		}
		if strings.HasSuffix(name, "_sealed") || strings.HasSuffix(name, "_enc") || name == "enc" {
			out = append(out, name)
		}
	}
	return out
}
