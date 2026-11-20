package main

import (
	"os"
	"strings"
	"testing"
)

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDebMetadata(t *testing.T) {
	s := fileText(t, "build/linux/nfpm/nfpm.yaml")
	for _, want := range []string{`maintainer: "Ciranda <hello@ciranda.dev>"`, `homepage: "https://kolo.ciranda.dev"`, `license: "Apache-2.0"`} {
		if !strings.Contains(s, want) {
			t.Errorf("nfpm.yaml lacks %s", want)
		}
	}
	for _, bad := range []string{"GIT_COMMITTER", "wails.io"} {
		if strings.Contains(s, bad) {
			t.Errorf("nfpm.yaml holds %s", bad)
		}
	}
}

func TestUninstallerDoesNotRemoveTheFolderRecursively(t *testing.T) {
	s := fileText(t, "build/windows/nsis/project.nsi")
	if strings.Contains(s, "RMDir /r $INSTDIR") || strings.Contains(s, `RMDir /r "$INSTDIR`) {
		t.Error("the uninstaller removes the install folder recursively")
	}
	if !strings.Contains(s, `Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"`) || !strings.Contains(s, "\n    RMDir $INSTDIR\n") {
		t.Error("the uninstaller must delete the executable and then remove the folder only when it is empty")
	}
}

func TestAppImageScriptPinsLinuxdeploy(t *testing.T) {
	s := fileText(t, "build/linux/appimage/build.sh")
	if strings.Contains(s, "continuous") {
		t.Error("linuxdeploy is downloaded from the moving continuous release")
	}
	for _, want := range []string{`LINUXDEPLOY_RELEASE="1-alpha-20251107-1"`, "sha256sum -c -", "c20cd71e3a4e3b80c3483cef793cda3f4e990aca14014d23c544ca3ce1270b4d", "620095110d693282b8ebeb244a95b5e911cf8f65f76c88b4b47d16ae6346fcff"} {
		if !strings.Contains(s, want) {
			t.Errorf("build.sh lacks %s", want)
		}
	}
	if strings.Index(s, "sha256sum -c -") > strings.Index(s, `"./${LINUXDEPLOY}" --appdir`) {
		t.Error("linuxdeploy runs before its checksum is checked")
	}
}
