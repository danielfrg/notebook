package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOpenVaults(t *testing.T) {
	specs := []VaultSpec{{"notes", t.TempDir()}, {"work", t.TempDir()}}
	for _, readOnly := range []bool{false, true} {
		reg, err := openVaults(Settings{
			Vaults:     specs,
			ReadOnly:   readOnly,
			MCPExclude: map[string][]string{"notes": {"transcripts", "Private/**"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, v := range reg.All() {
			names = append(names, v.Name())
			if v.ReadOnly() != readOnly {
				t.Errorf("vault %q ReadOnly = %v, want the setting, %v", v.Name(), v.ReadOnly(), readOnly)
			}
		}
		if want := []string{"notes", "work"}; !slices.Equal(names, want) {
			t.Errorf("vaults = %v, want %v, in the order they are configured", names, want)
		}
		if err := reg.Close(); err != nil {
			t.Error(err)
		}
	}
}

// Whatever is wrong with the vaults stops the start, with an error that names the
// variable and the vault, and not a server that runs with an exclude that hides
// nothing or with a vault missing.
func TestOpenVaultsFailsFast(t *testing.T) {
	dir := t.TempDir()
	both := []VaultSpec{{"notes", dir}, {"work", dir}}
	for _, tc := range []struct {
		name     string
		settings Settings
		want     []string
	}{
		{
			"a pattern that ExcludeGlobs refuses",
			Settings{Vaults: both, MCPExclude: map[string][]string{"notes": {"fine"}, "work": {"transcripts", "/abs"}}},
			[]string{"NOTEBOOK_MCP_EXCLUDE", `vault "work"`, `"/abs"`},
		},
		{
			"a directory that is not there",
			Settings{Vaults: []VaultSpec{{"notes", dir}, {"work", filepath.Join(dir, "missing")}}},
			[]string{"NOTEBOOK_VAULTS", `vault "work"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, err := openVaults(tc.settings)
			if err == nil {
				reg.Close()
				t.Fatal("openVaults succeeded, want an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %s", err, want)
				}
			}
			if reg != nil {
				t.Errorf("registry = %v, want none with the error", reg)
			}
		})
	}
}
