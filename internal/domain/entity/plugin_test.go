package entity

import "testing"

func TestPluginKindSandbox(t *testing.T) {
	if PluginKindSandbox != "sandbox" {
		t.Errorf("PluginKindSandbox = %q, want sandbox", PluginKindSandbox)
	}
}
