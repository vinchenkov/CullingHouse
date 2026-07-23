package deployment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeNamesPinDomainAndGrammar(t *testing.T) {
	names, err := RuntimeNames("/Users/alice/.mission-control")
	if err != nil {
		t.Fatal(err)
	}
	if names.Suffix != "a4a9410038b1" {
		t.Fatalf("suffix = %q, want pinned domain hash", names.Suffix)
	}
	if names.Volume != "mc-spine-a4a9410038b1" || names.Helper != "mc-helper-a4a9410038b1" {
		t.Fatalf("names = %#v", names)
	}
	if _, err := RuntimeNames("relative/home"); err == nil {
		t.Fatal("relative MC_HOME received a deployment identity")
	}
}

func TestCanonicalHomeMakesSymlinkAliasesOneDeployment(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Fatal(err)
	}
	realHome, err := CanonicalHome(filepath.Join(realParent, "missing", "home"))
	if err != nil {
		t.Fatal(err)
	}
	aliasHome, err := CanonicalHome(filepath.Join(alias, "missing", "home"))
	if err != nil {
		t.Fatal(err)
	}
	if realHome != aliasHome {
		t.Fatalf("canonical homes differ: %q != %q", realHome, aliasHome)
	}
	realNames, _ := RuntimeNames(realHome)
	aliasNames, _ := RuntimeNames(aliasHome)
	if realNames != aliasNames {
		t.Fatalf("symlink aliases split deployment identity: %#v != %#v", realNames, aliasNames)
	}
}

func TestDifferentCanonicalHomesCannotShareRuntimeObjects(t *testing.T) {
	a, err := RuntimeNames("/Users/alice/.mission-control")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RuntimeNames("/Users/alice/.mission-control-two")
	if err != nil {
		t.Fatal(err)
	}
	if a.Suffix == b.Suffix || a.Volume == b.Volume || a.Helper == b.Helper {
		t.Fatalf("different homes collided: %#v %#v", a, b)
	}
}
