package extmgr

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/i18n"
)

func TestMorePermissionsTextLanguages(t *testing.T) {
	one := []extension.PermissionLine{{Title: "访问更多网络", Detail: "api.example.com"}}
	if got := morePermissionsText("", one); got != "要多一项权限：访问更多网络（api.example.com）" {
		t.Fatalf("default: %q", got)
	}
	two := []extension.PermissionLine{{Title: "Network", Detail: "api.example.com"}, {Title: "Files"}}
	if got := morePermissionsText(i18n.En, two); got != "Asks for two more permissions: Network (api.example.com), Files" {
		t.Fatalf("en: %q", got)
	}
	if got := morePermissionsText(i18n.En, one[:1]); got != "Asks for one more permission: 访问更多网络 (api.example.com)" {
		t.Fatalf("en one: %q", got)
	}
	many := make([]extension.PermissionLine, 7)
	for i := range many {
		many[i].Title = "p"
	}
	if got := morePermissionsText(i18n.Ja, many); got != "権限が7件増えます：p、p、p、p、p、p、p" {
		t.Fatalf("ja: %q", got)
	}
}
