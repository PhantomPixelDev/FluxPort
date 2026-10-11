package config

import "testing"

func TestPublicDemoIsExplicit(t *testing.T) {
	t.Setenv("FLUXPORT_CONFIG", "")
	t.Setenv("FLUXPORT_ADDRESS", "0.0.0.0:4170")
	t.Setenv("FLUXPORT_ADMIN_PASSWORD", "")
	t.Setenv("FLUXPORT_PUBLIC_DEMO", "false")
	t.Setenv("FLUXPORT_API_KEY", "private-installation-key")
	if _, err := Load(); err == nil {
		t.Fatal("anonymous remote binding must remain opt-in")
	}
	t.Setenv("FLUXPORT_PUBLIC_DEMO", "true")
	c, err := Load()
	if err != nil || !c.PublicDemo || c.AdminPassword != "" || c.APIKey != "" {
		t.Fatal(c, err)
	}
	t.Setenv("FLUXPORT_PUBLIC_DEMO", "false")
	t.Setenv("FLUXPORT_ADDRESS", "127.0.0.1:4170")
	c, err = Load()
	if err != nil || c.APIKey != "private-installation-key" {
		t.Fatal("normal installation lost its API key", err)
	}
	t.Setenv("FLUXPORT_PUBLIC_DEMO", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("invalid demo flag accepted")
	}
}
