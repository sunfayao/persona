package appserver

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// A control socket Codex serves on Windows is an AF_UNIX file: private when its owner is this user and its access
// list lets nobody else in; a socket others may use is refused.
func TestPrivateSocketOnWindows(t *testing.T) {
	t.Parallel()
	client := New(Config{})
	dir, err := os.MkdirTemp("", "cxs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := restrictToMe(dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := privateSocket(socket); err != nil {
		t.Fatalf("a socket only this user may use: %v", err)
	}
	if err := grantEveryone(socket); err != nil {
		t.Fatal(err)
	}
	if privateSocket(socket) == nil {
		t.Fatal("a socket everyone may use is private")
	}
	if _, _, err := client.Endpoint("unix://" + socket); err == nil {
		t.Fatal("an endpoint everyone may use is accepted")
	}
}

// A drive path is a Unix endpoint, not a URL with a host and a port.
func TestValidateCodexEndpointTakesADrivePath(t *testing.T) {
	t.Parallel()
	client := New(Config{})
	dir, err := os.MkdirTemp("", "cxs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := restrictToMe(dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, _, err := client.Endpoint("unix://" + socket); err != nil {
		t.Fatalf("unix://%s: %v", socket, err)
	}
	if _, _, err := client.Endpoint("unix://" + socket + "?x=1"); err == nil {
		t.Fatal("a query is accepted")
	}
}

// restrictToMe gives path an access list of this user alone (as Codex makes its control directory).
func restrictToMe(path string) error {
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(me.User.Sid)},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// grantEveryone adds read and write for Everyone to path's access list.
func grantEveryone(path string) error {
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	old, _, _ := sd.DACL()
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ | windows.GENERIC_WRITE, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_WELL_KNOWN_GROUP, TrusteeValue: windows.TrusteeValueFromSID(everyone)},
	}}, old)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
